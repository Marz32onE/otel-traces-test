import http from "http";
import net from "net";
import WebSocket from "ws";
type Ws = WebSocket;
import { trace, context as otelContext } from "@opentelemetry/api";
import OtelWebSocket from "@marz32one/otel-ws";
import { initOtel } from "./otel.js";

type WsPayload = { text: string };
type WsAck = { ack: true; echo: string; traceId?: string };
type WsInternalSender = {
  sendFrame: (list: Buffer[], cb?: (err?: Error) => void) => void;
};
type WsWithInternals = Ws & { _sender?: WsInternalSender };
type SenderFrameFn = (data: Buffer, options: unknown) => Buffer[];

let lastTraceId: string | null = null;
const wsSendFrameDebug = process.env.WS_SENDFRAME_DEBUG === "1";
const OTEL_WS_PROTOCOL = "otel-ws";

function debugLog(message: string, meta?: unknown): void {
  if (!wsSendFrameDebug) return;
  // eslint-disable-next-line no-console
  console.debug(`[ws-node-backend] ${message}`, meta ?? "");
}

function json(res: http.ServerResponse, status: number, body: unknown) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(body));
}

function asWsPayload(raw: unknown): WsPayload | null {
  if (
    !raw ||
    typeof raw !== "object" ||
    Buffer.isBuffer(raw) ||
    Array.isArray(raw) ||
    raw instanceof ArrayBuffer
  ) {
    return null;
  }
  if (!("text" in raw)) return null;
  return { text: String((raw as { text?: unknown }).text ?? "") };
}

function hasOtelProtocol(req: http.IncomingMessage): boolean {
  const protocolHeader = req.headers["sec-websocket-protocol"];
  if (!protocolHeader) return false;
  const value = Array.isArray(protocolHeader)
    ? protocolHeader.join(",")
    : protocolHeader;
  return value
    .split(",")
    .map((token) => token.trim())
    .includes(OTEL_WS_PROTOCOL);
}

function rejectUpgrade(socket: net.Socket): void {
  const body = "Sec-WebSocket-Protocol 'otel-ws' is required for /otel-ws";
  socket.write(
    "HTTP/1.1 426 Upgrade Required\r\n" +
      "Connection: close\r\n" +
      "Content-Type: text/plain\r\n" +
      `Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n` +
      "\r\n" +
      body,
  );
  socket.destroy();
}

const port = Number(process.env.PORT ?? 8085);

const server = http.createServer((req, res) => {
  const url = new URL(
    req.url ?? "/",
    `http://${req.headers.host ?? "localhost"}`,
  );

  if (url.pathname === "/health") {
    json(res, 200, { status: "ok" });
    return;
  }

  if (url.pathname === "/last-trace-id") {
    json(res, 200, { traceId: lastTraceId });
    return;
  }

  json(res, 404, { error: "not found" });
});

async function main() {
  const { shutdown } = initOtel();

  // Use noServer + manual upgrade routing so both servers share one HTTP server
  // without ws v5's path-option bug (first server rejects all non-matching upgrades).
  const wssOtel = new OtelWebSocket.Server({
    noServer: true,
    perMessageDeflate: false,
  });
  const wssPlain = new WebSocket.Server({
    noServer: true,
    perMessageDeflate: false,
  });

  server.on("upgrade", (req, socket, head) => {
    const netSocket = socket as net.Socket;
    const pathname = new URL(
      req.url ?? "/",
      `http://${req.headers.host ?? "localhost"}`,
    ).pathname;
    if (pathname === "/otel-ws") {
      if (!hasOtelProtocol(req)) {
        rejectUpgrade(netSocket);
        return;
      }
      wssOtel.handleUpgrade(req, netSocket, head, (ws: Ws) =>
        ws.protocol === OTEL_WS_PROTOCOL
          ? wssOtel.emit("connection", ws, req)
          : ws.close(1002, "otel-ws protocol negotiation required"),
      );
    } else if (pathname === "/ws") {
      wssPlain.handleUpgrade(req, netSocket, head, (ws: Ws) =>
        wssPlain.emit("connection", ws, req),
      );
    } else {
      netSocket.destroy();
    }
  });

  wssOtel.on("connection", (ws: Ws) => {
    ws.on("message", (raw) => {
      const parsed = asWsPayload(raw);
      const data: WsPayload | string = parsed ?? String(raw);
      const ctx = otelContext.active();
      const sc = trace.getSpanContext(ctx);
      if (sc?.traceId) lastTraceId = sc.traceId;

      const echo =
        data && typeof data === "object" && "text" in data
          ? (data as WsPayload).text
          : String(data);
      const ack: WsAck = { ack: true, echo, traceId: lastTraceId ?? undefined };

      // Ensure send is linked to the context created by otel-ws's receive handling.
      otelContext.with(ctx, () => {
        const sender = (ws as WsWithInternals)._sender;
        const frameFn = (
          WebSocket as unknown as { Sender?: { frame?: SenderFrameFn } }
        ).Sender?.frame;
        if (
          !sender ||
          typeof sender.sendFrame !== "function" ||
          typeof frameFn !== "function"
        ) {
          debugLog("sendFrame unavailable, fallback to sock.send");
          ws.send(ack);
          return;
        }

        const payload = JSON.stringify(ack);
        const options = {
          fin: true,
          rsv1: false,
          opcode: 1,
          mask: false,
          readOnly: false,
        };

        try {
          const frameParts = frameFn(Buffer.from(payload, "utf8"), options);
          const mergedFrame = Buffer.concat(frameParts);
          sender.sendFrame([mergedFrame], (err?: Error) => {
            if (err) {
              debugLog("sendFrame failed, fallback to sock.send", {
                error: err.message,
              });
              ws.send(ack);
              return;
            }
            debugLog("sendFrame success");
          });
        } catch (err) {
          debugLog("sendFrame throw, fallback to sock.send", {
            error: (err as Error).message,
          });
          ws.send(ack);
        }
      });
    });
  });

  wssPlain.on("connection", (ws: Ws) => {
    ws.on("message", (data) => {
      ws.send(String(data));
    });
  });

  server.listen(port, () => {
    // eslint-disable-next-line no-console
    console.log(
      `ws-node-backend listening on http://0.0.0.0:${port} (ws paths: /otel-ws, /ws)`,
    );
  });

  const onSignal = async () => {
    server.close(() => undefined);
    await shutdown();
    process.exit(0);
  };

  process.on("SIGINT", onSignal);
  process.on("SIGTERM", onSignal);
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error("Fatal error:", err);
  process.exit(1);
});

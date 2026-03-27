import http from 'http';
import net from 'net';
import WebSocket from 'ws';
type Ws = WebSocket;
import { trace, context as otelContext } from '@opentelemetry/api';

import { instrumentSocket } from '@marz32one/otel-ws';
import { initOtel } from './otel.js';

type WsPayload = { text: string };
type WsAck = { ack: true; echo: string; traceId?: string };

let lastTraceId: string | null = null;

function json(res: http.ServerResponse, status: number, body: unknown) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(JSON.stringify(body));
}

const port = Number(process.env.PORT ?? 8085);

const server = http.createServer((req, res) => {
  const url = new URL(req.url ?? '/', `http://${req.headers.host ?? 'localhost'}`);

  if (url.pathname === '/health') {
    json(res, 200, { status: 'ok' });
    return;
  }

  if (url.pathname === '/last-trace-id') {
    json(res, 200, { traceId: lastTraceId });
    return;
  }

  json(res, 404, { error: 'not found' });
});

async function main() {
  const { shutdown } = initOtel();

  // Use noServer + manual upgrade routing so both servers share one HTTP server
  // without ws v5's path-option bug (first server rejects all non-matching upgrades).
  const wssOtel = new WebSocket.Server({ noServer: true, perMessageDeflate: false });
  const wssPlain = new WebSocket.Server({ noServer: true, perMessageDeflate: false });

  server.on('upgrade', (req, socket, head) => {
    const netSocket = socket as net.Socket;
    const pathname = new URL(req.url ?? '/', `http://${req.headers.host ?? 'localhost'}`).pathname;
    if (pathname === '/otel-ws') {
      wssOtel.handleUpgrade(req, netSocket, head, (ws) => wssOtel.emit('connection', ws, req));
    } else if (pathname === '/ws') {
      wssPlain.handleUpgrade(req, netSocket, head, (ws) => wssPlain.emit('connection', ws, req));
    } else {
      netSocket.destroy();
    }
  });

  wssOtel.on('connection', (ws: Ws) => {
    // - send(): WsAck
    // - onMessage handler input: WsPayload
    const sock = instrumentSocket<WsAck, WsPayload>(ws);

    sock.onMessage((data, ctx) => {
      const sc = trace.getSpanContext(ctx);
      if (sc?.traceId) lastTraceId = sc.traceId;

      const echo =
        data && typeof data === 'object' && 'text' in data
          ? (data as WsPayload).text
          : String(data);
      const ack: WsAck = { ack: true, echo, traceId: lastTraceId ?? undefined };

      // Ensure send is linked to the context created by otel-ws's receive handling.
      otelContext.with(ctx, () => {
        sock.send(ack);
      });
    });
  });

  wssPlain.on('connection', (ws: Ws) => {
    ws.on('message', (data) => {
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

  process.on('SIGINT', onSignal);
  process.on('SIGTERM', onSignal);
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('Fatal error:', err);
  process.exit(1);
});


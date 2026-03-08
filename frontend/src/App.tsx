import {
  useState,
  useEffect,
  useRef,
  type CSSProperties,
  type KeyboardEvent,
} from "react";
import {
  context,
  propagation,
  trace,
  SpanStatusCode,
  SpanKind,
} from "@opentelemetry/api";
import { tracer } from "./tracing";

const API_URL = import.meta.env.VITE_API_URL || "http://localhost:8088";
const WS_URL = import.meta.env.VITE_WS_URL || "ws://localhost:8082";

type LastTrace = { traceId: string; endpoint: string; id?: string } | null;

/** Parse trace ID from W3C traceparent (version-traceId-spanId-flags). */
function traceIdFromTraceparent(traceparent: string): string | null {
  const parts = traceparent.trim().split("-");
  return parts.length >= 2 ? (parts[1] ?? null) : null;
}

const DEFAULT_MONGO_ID = "_id";

export default function App() {
  const [natsInputText, setNatsInputText] = useState("");
  const [mongoInputText, setMongoInputText] = useState("");
  const [mongoId, setMongoId] = useState(DEFAULT_MONGO_ID);
  const [messages, setMessages] = useState<string[]>([]);
  const [wsStatus, setWsStatus] = useState("Connecting...");
  const [lastTrace, setLastTrace] = useState<LastTrace>(null);
  const [lastMongoId, setLastMongoId] = useState<string | null>(null);
  const [lastReceivedTraceId, setLastReceivedTraceId] = useState<string | null>(
    null,
  );
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    connectWS();
    return () => {
      if (wsRef.current) wsRef.current.close();
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
    };
  }, []);

  function connectWS() {
    const ws = new WebSocket(`${WS_URL}/ws`);
    wsRef.current = ws;

    ws.onopen = () => setWsStatus("Connected");
    ws.onclose = () => {
      setWsStatus("Reconnecting...");
      reconnectTimerRef.current = setTimeout(connectWS, 3000);
    };
    ws.onerror = () => setWsStatus("Error");
    ws.onmessage = (event: MessageEvent) => {
      const data = typeof event.data === "string" ? event.data : "";
      let body = data;
      let ctx = context.active();

      let displayText = body;
      try {
        const parsed = JSON.parse(data) as {
          traceparent?: string;
          tracestate?: string;
          body?: string;
          api?: string;
        };
        if (parsed.traceparent && parsed.body !== undefined) {
          body = parsed.body;
          displayText = parsed.api
            ? `${parsed.body} [${parsed.api}]`
            : parsed.body;
          const tid = traceIdFromTraceparent(parsed.traceparent);
          if (tid) setLastReceivedTraceId(tid);
          const carrier: Record<string, string> = {};
          if (parsed.traceparent) carrier.traceparent = parsed.traceparent;
          if (parsed.tracestate) carrier.tracestate = parsed.tracestate;
          ctx = propagation.extract(ctx, carrier);
        }
      } catch {
        /* plain text message, no trace */
      }

      context.with(ctx, () => {
        const span = tracer.startSpan("receive message", {
          kind: SpanKind.CONSUMER,
          attributes: {
            "message.content": body,
            "messaging.operation": "receive",
          },
        });
        span.setStatus({ code: SpanStatusCode.OK });
        span.end();
      });
      setMessages((prev: string[]) => [...prev, displayText]);
    };
  }

  async function sendToEndpoint(
    endpoint: string,
    spanName: string,
    body?: { text?: string; id?: string },
  ) {
    const payload: { text?: string; id?: string } = body ?? {};
    if (!("id" in payload) && !("text" in payload)) return;
    const needsText = [
      "/api/message",
      "/api/message-core",
      "/api/message-mongo",
    ].includes(endpoint);
    if (needsText && (!payload.text || !payload.text.trim())) return;

    const url = `${API_URL}${endpoint}`;
    const span = tracer.startSpan(spanName, {
      kind: SpanKind.CLIENT,
      attributes: {
        "message.content": "text" in payload ? (payload.text ?? "") : "",
        "http.request.method": "POST",
        "url.full": url,
      },
    });
    const ctx = trace.setSpan(context.active(), span);

    try {
      const res = await context.with(ctx, () =>
        fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        }),
      );
      span.setAttribute("http.response.status_code", res.status);
      const data = (await res.json()) as {
        trace_id?: string;
        endpoint?: string;
        id?: string;
        text?: string;
        error?: string;
      };
      if (!res.ok) {
        throw new Error(data.error ?? "Request failed");
      }
      span.setStatus({ code: SpanStatusCode.OK });
      if (
        "text" in payload &&
        payload.text &&
        (endpoint === "/api/message" || endpoint === "/api/message-core")
      )
        setNatsInputText("");
      if (
        "text" in payload &&
        payload.text &&
        endpoint === "/api/message-mongo"
      )
        setMongoInputText("");
      if (data.trace_id) {
        setLastTrace({
          traceId: data.trace_id,
          endpoint: data.endpoint ?? endpoint,
          id: data.id,
        });
        if (data.id) {
          setLastMongoId(data.id);
          setMongoId(data.id); // sync id field for update/read/delete
        }
        if (data.endpoint === "MongoDB Delete") setLastMongoId(null);
      }
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      span.setStatus({ code: SpanStatusCode.ERROR, message: error.message });
      span.recordException(error);
      alert(`Error: ${error.message}`);
    } finally {
      span.end();
    }
  }

  function handleNatsKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter")
      sendToEndpoint("/api/message", "send-message-jetstream", {
        text: natsInputText.trim(),
      });
  }

  function handleMongoKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter")
      sendToEndpoint("/api/message-mongo", "send-message-mongo", {
        text: mongoInputText.trim(),
      });
  }

  return (
    <div style={styles.container}>
      <h1 style={styles.title}>Message Demo</h1>
      <p style={styles.status}>
        WebSocket: <strong>{wsStatus}</strong>
      </p>

      <div style={styles.twoColumns}>
        {/* Left: NATS */}
        <div style={styles.panel}>
          <h2 style={styles.panelTitle}>NATS</h2>
          <div style={styles.inputRow}>
            <input
              style={styles.input}
              type="text"
              placeholder="Enter a message..."
              value={natsInputText}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                setNatsInputText(e.target.value)
              }
              onKeyDown={handleNatsKeyDown}
            />
          </div>
          <div style={styles.buttonRow}>
            <button
              style={styles.button}
              onClick={() =>
                sendToEndpoint("/api/message", "send-message-jetstream", {
                  text: natsInputText.trim(),
                })
              }
              title="JetStream（natstrace 包裝 jetstream pkg）"
            >
              送出（JetStream）
            </button>
            <button
              style={{ ...styles.button, ...styles.buttonSecondary }}
              onClick={() =>
                sendToEndpoint("/api/message-core", "send-message-core", {
                  text: natsInputText.trim(),
                })
              }
              title="Core NATS fire-and-go"
            >
              送出（Core NATS）
            </button>
          </div>
          <div style={styles.traceFlow}>
            <span style={styles.traceFlowLabel}>Trace 經過：</span>
            <span style={styles.traceFlowPath}>
              Frontend → API → NATS (JetStream/Core) → Worker → WebSocket
            </span>
          </div>
        </div>

        {/* Right: MongoDB */}
        <div style={styles.panel}>
          <h2 style={styles.panelTitle}>MongoDB</h2>
          <div style={styles.inputRow}>
            <input
              style={styles.input}
              type="text"
              placeholder="Enter message text..."
              value={mongoInputText}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                setMongoInputText(e.target.value)
              }
              onKeyDown={handleMongoKeyDown}
            />
          </div>
          <div style={styles.idRow}>
            <label style={styles.idLabel}>ID</label>
            <input
              style={styles.idInput}
              type="text"
              value={mongoId}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                setMongoId(e.target.value || DEFAULT_MONGO_ID)
              }
              placeholder={DEFAULT_MONGO_ID}
              title="文件 _id（更新/讀取/刪除用），預設 trace_test"
            />
          </div>
          <div style={styles.buttonRow}>
            <button
              style={{ ...styles.button, ...styles.buttonTertiary }}
              onClick={() =>
                sendToEndpoint("/api/message-mongo", "send-message-mongo", {
                  text: mongoInputText.trim(),
                })
              }
              title="經 API 寫入 MongoDB（Insert），由 dbwatcher 監聽並轉發"
            >
              插入
            </button>
            <button
              style={{ ...styles.button, ...styles.buttonMongo }}
              onClick={() =>
                sendToEndpoint(
                  "/api/message-mongo-update",
                  "send-message-mongo-update",
                  {
                    id: mongoId.trim() || DEFAULT_MONGO_ID,
                    text: mongoInputText.trim() || "(updated)",
                  },
                )
              }
              title="以指定 id 更新文件（Update），會更換 _oteltrace"
            >
              更新
            </button>
            <button
              style={{ ...styles.button, ...styles.buttonMongo }}
              onClick={() =>
                sendToEndpoint(
                  "/api/message-mongo-read",
                  "send-message-mongo-read",
                  {
                    id: mongoId.trim() || DEFAULT_MONGO_ID,
                  },
                )
              }
              title="以指定 id 讀取文件（Read），span link 至文件內 _oteltrace"
            >
              讀取
            </button>
            <button
              style={{ ...styles.button, ...styles.buttonMongo }}
              onClick={() =>
                sendToEndpoint(
                  "/api/message-mongo-delete",
                  "send-message-mongo-delete",
                  {
                    id: mongoId.trim() || DEFAULT_MONGO_ID,
                  },
                )
              }
              title="以指定 id 刪除文件（Delete）"
            >
              刪除
            </button>
          </div>
          <div style={styles.traceFlow}>
            <span style={styles.traceFlowLabel}>Trace 經過：</span>
            <span style={styles.traceFlowPath}>
              Frontend → API → MongoDB → dbwatcher → NATS → Worker → WebSocket
            </span>
          </div>
        </div>
      </div>

      <div style={styles.resultsSection}>
        <h3 style={styles.resultsTitle}>
          由 WebSocket / Worker 監聽 NATS 取出的結果
        </h3>
        <textarea
          style={styles.resultsTextarea}
          readOnly
          value={messages.join("\n")}
          placeholder="訊息會經 Worker 從 NATS 訂閱後透過 WebSocket 送達並顯示於此..."
        />
      </div>

      <div style={styles.traceRow}>
        {lastTrace && (
          <div style={styles.traceVerify}>
            <strong>Trace 驗證（{lastTrace.endpoint}）</strong>
            <br />
            <code
              style={styles.traceId}
              title="在 Grafana/Tempo 用此 Trace ID 查詢"
            >
              {lastTrace.traceId}
            </code>
          </div>
        )}
        {lastMongoId && (
          <div style={styles.traceVerify}>
            <strong>最後插入的 Mongo ID</strong>
            <br />
            <code style={styles.traceId}>{lastMongoId}</code>
          </div>
        )}
        {lastReceivedTraceId && (
          <div style={styles.traceVerify}>
            <strong>最後收到訊息的 Trace ID</strong>
            <br />
            <code style={styles.traceId}>{lastReceivedTraceId}</code>
          </div>
        )}
      </div>
    </div>
  );
}

const styles: Record<string, CSSProperties> = {
  container: {
    maxWidth: "1000px",
    margin: "40px auto",
    fontFamily: "sans-serif",
    padding: "0 16px",
  },
  title: { marginBottom: "8px" },
  status: { marginBottom: "16px", color: "#555" },
  twoColumns: {
    display: "flex",
    flexDirection: "row" as const,
    gap: "24px",
    flexWrap: "wrap" as const,
  },
  panel: {
    flex: "1 1 400px",
    minWidth: "280px",
    padding: "16px",
    border: "1px solid #ddd",
    borderRadius: "8px",
    background: "#fafafa",
  },
  panelTitle: {
    marginTop: 0,
    marginBottom: "12px",
    fontSize: "18px",
  },
  traceRow: {
    display: "flex",
    flexDirection: "row" as const,
    flexWrap: "wrap" as const,
    gap: "12px",
    marginTop: "16px",
    marginBottom: "12px",
  },
  traceVerify: {
    padding: "8px 12px",
    fontSize: "13px",
    background: "#f5f5f5",
    borderRadius: "4px",
    wordBreak: "break-all" as const,
  },
  traceId: { fontSize: "12px", userSelect: "all" as const },
  inputRow: { display: "flex", gap: "8px", marginBottom: "8px" },
  idRow: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    marginBottom: "8px",
  },
  idLabel: {
    fontSize: "14px",
    fontWeight: "bold" as const,
    minWidth: "24px",
  },
  idInput: {
    flex: 1,
    padding: "6px 10px",
    fontSize: "14px",
    border: "1px solid #ccc",
    borderRadius: "4px",
  },
  buttonRow: {
    display: "flex",
    gap: "8px",
    marginBottom: "12px",
    flexWrap: "wrap" as const,
  },
  input: {
    flex: 1,
    padding: "8px 12px",
    fontSize: "16px",
    border: "1px solid #ccc",
    borderRadius: "4px",
  },
  button: {
    padding: "8px 16px",
    fontSize: "14px",
    backgroundColor: "#4f46e5",
    color: "#fff",
    border: "none",
    borderRadius: "4px",
    cursor: "pointer",
  },
  buttonSecondary: {
    backgroundColor: "#0d9488",
  },
  buttonTertiary: {
    backgroundColor: "#b45309",
  },
  buttonMongo: {
    backgroundColor: "#6b21a8",
  },
  traceFlow: {
    marginTop: "12px",
    padding: "8px 10px",
    fontSize: "12px",
    background: "#eee",
    borderRadius: "4px",
    borderLeft: "3px solid #4f46e5",
  },
  traceFlowLabel: {
    fontWeight: "bold" as const,
    marginRight: "6px",
  },
  traceFlowPath: {
    color: "#333",
  },
  resultsSection: {
    marginTop: "24px",
    padding: "16px",
    border: "1px solid #ccc",
    borderRadius: "8px",
    background: "#f9f9f9",
  },
  resultsTitle: {
    marginTop: 0,
    marginBottom: "10px",
    fontSize: "14px",
    color: "#555",
    fontWeight: "bold" as const,
  },
  resultsTextarea: {
    width: "100%",
    height: "260px",
    padding: "8px 12px",
    fontSize: "14px",
    border: "1px solid #ccc",
    borderRadius: "4px",
    resize: "vertical",
    boxSizing: "border-box",
  },
};

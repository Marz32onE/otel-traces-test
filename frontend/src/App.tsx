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

type LastTrace = { traceId: string; endpoint: string } | null;

/** Parse trace ID from W3C traceparent (version-traceId-spanId-flags). */
function traceIdFromTraceparent(traceparent: string): string | null {
  const parts = traceparent.trim().split("-");
  return parts.length >= 2 ? parts[1] ?? null : null;
}

export default function App() {
  const [inputText, setInputText] = useState("");
  const [messages, setMessages] = useState<string[]>([]);
  const [wsStatus, setWsStatus] = useState("Connecting...");
  const [lastTrace, setLastTrace] = useState<LastTrace>(null);
  const [lastReceivedTraceId, setLastReceivedTraceId] = useState<string | null>(
    null
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

  async function sendToEndpoint(endpoint: string, spanName: string) {
    const text = inputText.trim();
    if (!text) return;

    const url = `${API_URL}${endpoint}`;
    const span = tracer.startSpan(spanName, {
      kind: SpanKind.CLIENT,
      attributes: {
        "message.content": text,
        "http.request.method": "POST",
        "url.full": url,
      },
    });
    const ctx = trace.setSpan(context.active(), span);

    try {
      // Faro TracingInstrumentation injects traceparent/tracestate into fetch automatically when run inside this context
      const res = await context.with(ctx, () =>
        fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ text }),
        })
      );
      span.setAttribute("http.response.status_code", res.status);
      if (!res.ok) throw new Error("Failed to send");
      span.setStatus({ code: SpanStatusCode.OK });
      setInputText("");
      try {
        const data = (await res.json()) as {
          trace_id?: string;
          endpoint?: string;
        };
        if (data.trace_id) {
          setLastTrace({
            traceId: data.trace_id,
            endpoint: data.endpoint ?? endpoint,
          });
        }
      } catch {
        /* ignore */
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

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter")
      sendToEndpoint("/api/message", "send-message-jetstream");
  }

  return (
    <div style={styles.container}>
      <h1 style={styles.title}>Message Demo</h1>
      <p style={styles.status}>
        WebSocket: <strong>{wsStatus}</strong>
      </p>
      <div style={styles.inputRow}>
        <input
          style={styles.input}
          type="text"
          placeholder="Enter a message..."
          value={inputText}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
            setInputText(e.target.value)
          }
          onKeyDown={handleKeyDown}
        />
      </div>
      <div style={styles.buttonRow}>
        <button
          style={styles.button}
          onClick={() =>
            sendToEndpoint("/api/message", "send-message-jetstream")
          }
          title="JetStream（natstrace 包裝 jetstream pkg）"
        >
          送出（JetStream）
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonSecondary }}
          onClick={() =>
            sendToEndpoint("/api/message-core", "send-message-core")
          }
          title="Core NATS fire-and-go"
        >
          送出（Core NATS fire-and-go）
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonTertiary }}
          onClick={() =>
            sendToEndpoint("/api/message-mongo", "send-message-mongo")
          }
          title="經 API 寫入 MongoDB，由 dbwatcher 監聽並轉發至 NATS JetStream"
        >
          送出（MongoDB）
        </button>
      </div>
      <div style={styles.traceRow}>
        {lastTrace && (
          <div style={styles.traceVerify}>
            <strong>Trace 驗證（{lastTrace.endpoint}）</strong>
            <br />
            <code
              style={styles.traceId}
              title="在 Grafana/Tempo 用此 Trace ID 查詢，可看到 Frontend → API → Producer → Worker Consumer → WS 串聯"
            >
              {lastTrace.traceId}
            </code>
          </div>
        )}
        {lastReceivedTraceId && (
          <div style={styles.traceVerify}>
            <strong>最後收到訊息的 Trace ID</strong>
            <br />
            <code
              style={styles.traceId}
              title="此訊息經 WebSocket 送達時所帶的 traceparent 中的 Trace ID，可與上方比對是否為同一條 trace"
            >
              {lastReceivedTraceId}
            </code>
          </div>
        )}
      </div>
      <textarea
        style={styles.textarea}
        readOnly
        value={messages.join("\n")}
        placeholder="Messages will appear here..."
      />
    </div>
  );
}

const styles: Record<string, CSSProperties> = {
  container: {
    maxWidth: "600px",
    margin: "40px auto",
    fontFamily: "sans-serif",
    padding: "0 16px",
  },
  title: { marginBottom: "8px" },
  status: { marginBottom: "16px", color: "#555" },
  traceRow: {
    display: "flex",
    flexDirection: "row" as const,
    flexWrap: "wrap" as const,
    gap: "12px",
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
  buttonRow: {
    display: "flex",
    gap: "8px",
    marginBottom: "16px",
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
  textarea: {
    width: "100%",
    height: "300px",
    padding: "8px 12px",
    fontSize: "15px",
    border: "1px solid #ccc",
    borderRadius: "4px",
    resize: "vertical",
    boxSizing: "border-box",
  },
};

import { useState, useCallback } from "react";
import type { KeyboardEvent } from "react";
import { useWebSocket } from "./hooks/useWebSocket";
import { useMessageSender } from "./hooks/useMessageSender";
import {
  MessagePanel,
  MongoPanel,
  ResultsSection,
  TraceVerification,
} from "./components";
import { styles } from "./styles";
import { DEFAULT_MONGO_ID } from "./constants/endpoints";

export default function App() {
  const [natsInputText, setNatsInputText] = useState("");
  const [workerHttpInputText, setWorkerHttpInputText] = useState("");
  const [mongoInputText, setMongoInputText] = useState("");

  const { messages, status, lastReceivedTraceId } = useWebSocket();
  const {
    sendToEndpoint,
    lastTrace,
    lastMongoId,
    mongoId,
    setMongoId,
  } = useMessageSender();

  const send = useCallback(
    (
      endpoint: string,
      spanName: string,
      body: { text?: string; id?: string },
      onSuccess?: () => void,
    ) => {
      sendToEndpoint(endpoint, spanName, body, { onSuccess });
    },
    [sendToEndpoint],
  );

  const handleNatsKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter")
        send("/api/message", "send-message-jetstream", { text: natsInputText.trim() }, () =>
          setNatsInputText(""),
        );
    },
    [natsInputText, send],
  );

  const handleWorkerHttpKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter")
        send(
          "/api/message-via-worker",
          "send-message-via-worker-http",
          { text: workerHttpInputText.trim() },
          () => setWorkerHttpInputText(""),
        );
    },
    [workerHttpInputText, send],
  );

  const handleMongoKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter")
        send("/api/message-mongo", "send-message-mongo", { text: mongoInputText.trim() }, () =>
          setMongoInputText(""),
        );
    },
    [mongoInputText, send],
  );

  return (
    <div style={styles.container}>
      <h1 style={styles.title}>Message Demo</h1>
      <p style={styles.status}>
        WebSocket: <strong>{status}</strong>
      </p>

      <div style={styles.twoColumns}>
        <MessagePanel
          title="NATS"
          value={natsInputText}
          onChange={setNatsInputText}
          onKeyDown={handleNatsKeyDown}
          buttons={[
            {
              label: "送出（JetStream）",
              title: "JetStream（natstrace 包裝 jetstream pkg）",
              onClick: () =>
                send("/api/message", "send-message-jetstream", { text: natsInputText.trim() }, () =>
                  setNatsInputText(""),
                ),
            },
            {
              label: "送出（Core NATS）",
              variant: "secondary",
              title: "Core NATS fire-and-go",
              onClick: () =>
                send("/api/message-core", "send-message-core", { text: natsInputText.trim() }, () =>
                  setNatsInputText(""),
                ),
            },
          ]}
          traceFlowText="Frontend → API → NATS (JetStream/Core) → Worker → WebSocket"
        />

        <MessagePanel
          title="Worker HTTP"
          value={workerHttpInputText}
          onChange={setWorkerHttpInputText}
          onKeyDown={handleWorkerHttpKeyDown}
          buttons={[
            {
              label: "送出（Worker HTTP）",
              variant: "secondary",
              title: "API 以 otelresty 呼叫 Worker POST /notify（HTTP 追蹤）",
              onClick: () =>
                send(
                  "/api/message-via-worker",
                  "send-message-via-worker-http",
                  { text: workerHttpInputText.trim() },
                  () => setWorkerHttpInputText(""),
                ),
            },
          ]}
          traceFlowText="Frontend → API → Worker /notify (otelresty)"
        />

        <MongoPanel
          mongoInputText={mongoInputText}
          setMongoInputText={setMongoInputText}
          mongoId={mongoId}
          setMongoId={setMongoId}
          onMongoKeyDown={handleMongoKeyDown}
          onSendInsert={() =>
            send("/api/message-mongo", "send-message-mongo", { text: mongoInputText.trim() }, () =>
              setMongoInputText(""),
            )
          }
          onSendUpdate={() =>
            send("/api/message-mongo-update", "send-message-mongo-update", {
              id: mongoId.trim() || DEFAULT_MONGO_ID,
              text: mongoInputText.trim() || "(updated)",
            })
          }
          onSendRead={() =>
            send("/api/message-mongo-read", "send-message-mongo-read", {
              id: mongoId.trim() || DEFAULT_MONGO_ID,
            })
          }
          onSendDelete={() =>
            send("/api/message-mongo-delete", "send-message-mongo-delete", {
              id: mongoId.trim() || DEFAULT_MONGO_ID,
            })
          }
        />
      </div>

      <ResultsSection messages={messages} />

      <TraceVerification
        lastTrace={lastTrace}
        lastMongoId={lastMongoId}
        lastReceivedTraceId={lastReceivedTraceId}
      />
    </div>
  );
}

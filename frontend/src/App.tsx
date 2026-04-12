import { useState, useCallback } from "react";
import type { KeyboardEvent } from "react";
import { useWebSocket } from "./hooks/useWebSocket";
import { useMessageSender } from "./hooks/useMessageSender";
import { useNatsBrowser } from "./hooks/useNatsBrowser";
import {
  MessagePanel,
  MongoPanel,
  ResultsSection,
  TraceVerification,
} from "./components";
import { styles } from "./styles";
import { DEFAULT_MONGO_ID } from "./constants/endpoints";
import { API_V1_URL } from "./constants/env";

export default function App() {
  const [natsInputText, setNatsInputText] = useState("");
  const [natsBrowserInputText, setNatsBrowserInputText] = useState("");
  const [workerHttpInputText, setWorkerHttpInputText] = useState("");
  const [mongoInputText, setMongoInputText] = useState("");
  const [mongoInputTextV1, setMongoInputTextV1] = useState("");

  const { messages, status, lastReceivedTraceId } = useWebSocket();
  const {
    natsWsStatus,
    natsReady,
    publishJetStream: publishNatsBrowserJs,
    publishCore: publishNatsBrowserCore,
  } = useNatsBrowser();
  const {
    sendToEndpoint,
    lastTrace,
    lastMongoId,
    lastBulkInsertIds,
    mongoId,
    setMongoId,
  } = useMessageSender();
  const {
    sendToEndpoint: sendToEndpointV1,
    lastTrace: lastTraceV1,
    lastMongoId: lastMongoIdV1,
    lastBulkInsertIds: lastBulkInsertIdsV1,
    mongoId: mongoIdV1,
    setMongoId: setMongoIdV1,
  } = useMessageSender(API_V1_URL);

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

  const handleNatsBrowserKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key !== "Enter" || !natsReady) return;
      void publishNatsBrowserJs(natsBrowserInputText)
        .then(() => setNatsBrowserInputText(""))
        .catch(() => undefined);
    },
    [natsBrowserInputText, natsReady, publishNatsBrowserJs],
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

  const sendV1 = useCallback(
    (
      endpoint: string,
      spanName: string,
      body: { text?: string; id?: string },
      onSuccess?: () => void,
    ) => {
      sendToEndpointV1(endpoint, spanName, body, { onSuccess });
    },
    [sendToEndpointV1],
  );

  const handleMongoKeyDownV1 = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter")
        sendV1("/api/message-mongo", "send-message-mongo-v1", { text: mongoInputTextV1.trim() }, () =>
          setMongoInputTextV1(""),
        );
    },
    [mongoInputTextV1, sendV1],
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
          title="NATS（瀏覽器 / otel-nats）"
          value={natsBrowserInputText}
          onChange={setNatsBrowserInputText}
          onKeyDown={handleNatsBrowserKeyDown}
          connectionStatus={natsWsStatus}
          buttons={[
            {
              label: "送出（JetStream）",
              title: "wsconnect + createJetStream — subject messages.new",
              onClick: () =>
                void publishNatsBrowserJs(natsBrowserInputText)
                  .then(() => setNatsBrowserInputText(""))
                  .catch(() => undefined),
              disabled: !natsReady,
            },
            {
              label: "送出（Core NATS）",
              variant: "secondary",
              title: "wsconnect — subject messages.core",
              onClick: () => {
                try {
                  publishNatsBrowserCore(natsBrowserInputText);
                  setNatsBrowserInputText("");
                } catch {
                  /* alert in hook */
                }
              },
              disabled: !natsReady,
            },
          ]}
          traceFlowText="Frontend → NATS (WebSocket) → Worker → WebSocket（無 API HTTP）"
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
          title="MongoDB (v2)"
          traceFlowPath="Frontend → API (v2) → MongoDB → dbwatcher → NATS → Worker → WebSocket"
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
          onSendBulkInsert={() =>
            sendToEndpoint(
              "/api/message-mongo-bulk-insert",
              "send-message-mongo-bulk-insert",
              { texts: ["Bulk insert 1", "Bulk insert 2", "Bulk insert 3"] },
            )
          }
          onSendBulkUpdate={() => {
            const id = mongoId.trim() || DEFAULT_MONGO_ID;
            const text = mongoInputText.trim() || "Bulk updated";
            const updates =
              lastBulkInsertIds.length >= 2
                ? lastBulkInsertIds.slice(0, 3).map((bulkId, i) => ({
                    id: bulkId,
                    text: i === 0 ? text : `Updated ${i + 1}`,
                  }))
                : [
                    { id, text },
                    { id, text: "Bulk update 2" },
                  ];
            sendToEndpoint(
              "/api/message-mongo-bulk-update",
              "send-message-mongo-bulk-update",
              { updates },
            );
          }}
        />

        <MongoPanel
          title="MongoDB (v1)"
          traceFlowPath="Frontend → API (v1) → MongoDB (otelmongo v1) → dbwatcher → NATS → Worker → WebSocket"
          mongoInputText={mongoInputTextV1}
          setMongoInputText={setMongoInputTextV1}
          mongoId={mongoIdV1}
          setMongoId={setMongoIdV1}
          onMongoKeyDown={handleMongoKeyDownV1}
          onSendInsert={() =>
            sendV1("/api/message-mongo", "send-message-mongo-v1", { text: mongoInputTextV1.trim() }, () =>
              setMongoInputTextV1(""),
            )
          }
          onSendUpdate={() =>
            sendV1("/api/message-mongo-update", "send-message-mongo-update-v1", {
              id: mongoIdV1.trim() || DEFAULT_MONGO_ID,
              text: mongoInputTextV1.trim() || "(updated)",
            })
          }
          onSendRead={() =>
            sendV1("/api/message-mongo-read", "send-message-mongo-read-v1", {
              id: mongoIdV1.trim() || DEFAULT_MONGO_ID,
            })
          }
          onSendDelete={() =>
            sendV1("/api/message-mongo-delete", "send-message-mongo-delete-v1", {
              id: mongoIdV1.trim() || DEFAULT_MONGO_ID,
            })
          }
          onSendBulkInsert={() =>
            sendToEndpointV1(
              "/api/message-mongo-bulk-insert",
              "send-message-mongo-bulk-insert-v1",
              { texts: ["Bulk insert v1-1", "Bulk insert v1-2", "Bulk insert v1-3"] },
            )
          }
          onSendBulkUpdate={() => {
            const id = mongoIdV1.trim() || DEFAULT_MONGO_ID;
            const text = mongoInputTextV1.trim() || "Bulk updated v1";
            const updates =
              lastBulkInsertIdsV1.length >= 2
                ? lastBulkInsertIdsV1.slice(0, 3).map((bulkId, i) => ({
                    id: bulkId,
                    text: i === 0 ? text : `Updated v1 ${i + 1}`,
                  }))
                : [
                    { id, text },
                    { id, text: "Bulk update v1 2" },
                  ];
            sendToEndpointV1(
              "/api/message-mongo-bulk-update",
              "send-message-mongo-bulk-update-v1",
              { updates },
            );
          }}
        />
      </div>

      <ResultsSection messages={messages} />

      <TraceVerification
        lastTrace={lastTrace}
        lastMongoId={lastMongoId}
        lastReceivedTraceId={lastReceivedTraceId}
        lastTraceV1={lastTraceV1}
        lastMongoIdV1={lastMongoIdV1}
      />
    </div>
  );
}

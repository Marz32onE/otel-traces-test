import { styles } from "../../styles";
import type { LastTrace } from "../../types";

type TraceVerificationProps = {
  lastTrace: LastTrace;
  lastMongoId: string | null;
  lastReceivedTraceId: string | null;
  lastTraceV1?: LastTrace | null;
  lastMongoIdV1?: string | null;
};

export function TraceVerification({
  lastTrace,
  lastMongoId,
  lastReceivedTraceId,
  lastTraceV1 = null,
  lastMongoIdV1 = null,
}: TraceVerificationProps) {
  return (
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
          <strong>最後插入的 Mongo ID (v2)</strong>
          <br />
          <code style={styles.traceId}>{lastMongoId}</code>
        </div>
      )}
      {lastTraceV1 && (
        <div style={styles.traceVerify}>
          <strong>Trace 驗證 v1（{lastTraceV1.endpoint}）</strong>
          <br />
          <code
            style={styles.traceId}
            title="在 Grafana/Tempo 用此 Trace ID 查詢"
          >
            {lastTraceV1.traceId}
          </code>
        </div>
      )}
      {lastMongoIdV1 && (
        <div style={styles.traceVerify}>
          <strong>最後插入的 Mongo ID (v1)</strong>
          <br />
          <code style={styles.traceId}>{lastMongoIdV1}</code>
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
  );
}

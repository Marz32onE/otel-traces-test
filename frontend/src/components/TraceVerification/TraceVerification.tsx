import { styles } from "../../styles";
import type { LastTrace } from "../../types";

type TraceVerificationProps = {
  lastTrace: LastTrace;
  lastMongoId: string | null;
  lastReceivedTraceId: string | null;
};

export function TraceVerification({
  lastTrace,
  lastMongoId,
  lastReceivedTraceId,
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
  );
}

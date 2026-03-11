import { styles } from "../../styles";

type ResultsSectionProps = {
  messages: string[];
};

export function ResultsSection({ messages }: ResultsSectionProps) {
  return (
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
  );
}

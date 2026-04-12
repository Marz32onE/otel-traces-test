import type { KeyboardEvent, ChangeEvent } from "react";
import { styles } from "../../styles";

export type PanelButton = {
  label: string;
  onClick: () => void;
  title?: string;
  variant?: "primary" | "secondary";
  disabled?: boolean;
};

type MessagePanelProps = {
  title: string;
  placeholder?: string;
  value: string;
  onChange: (value: string) => void;
  onKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
  buttons: PanelButton[];
  traceFlowText: string;
  /** Optional line under the title (e.g. NATS WebSocket connection state). */
  connectionStatus?: string;
};

export function MessagePanel({
  title,
  placeholder = "Enter a message...",
  value,
  onChange,
  onKeyDown,
  buttons,
  traceFlowText,
  connectionStatus,
}: MessagePanelProps) {
  return (
    <div style={styles.panel}>
      <h2 style={styles.panelTitle}>{title}</h2>
      {connectionStatus ? (
        <p style={{ margin: "0 0 8px", fontSize: 12, opacity: 0.9 }}>
          {connectionStatus}
        </p>
      ) : null}
      <div style={styles.inputRow}>
        <input
          style={styles.input}
          type="text"
          placeholder={placeholder}
          value={value}
          onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
          onKeyDown={onKeyDown}
        />
      </div>
      <div style={styles.buttonRow}>
        {buttons.map((btn) => (
          <button
            key={btn.label}
            style={
              btn.variant === "secondary"
                ? { ...styles.button, ...styles.buttonSecondary }
                : styles.button
            }
            onClick={btn.onClick}
            title={btn.title}
            disabled={btn.disabled}
          >
            {btn.label}
          </button>
        ))}
      </div>
      <div style={styles.traceFlow}>
        <span style={styles.traceFlowLabel}>Trace 經過：</span>
        <span style={styles.traceFlowPath}>{traceFlowText}</span>
      </div>
    </div>
  );
}

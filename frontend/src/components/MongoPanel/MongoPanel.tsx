import type { ChangeEvent, KeyboardEvent } from "react";
import { styles } from "../../styles";
import { DEFAULT_MONGO_ID } from "../../constants/endpoints";

type MongoPanelProps = {
  mongoInputText: string;
  setMongoInputText: (v: string) => void;
  mongoId: string;
  setMongoId: (v: string) => void;
  onSendInsert: () => void;
  onSendUpdate: () => void;
  onSendRead: () => void;
  onSendDelete: () => void;
  onSendBulkInsert: () => void;
  onSendBulkUpdate: () => void;
  onMongoKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
};

export function MongoPanel({
  mongoInputText,
  setMongoInputText,
  mongoId,
  setMongoId,
  onSendInsert,
  onSendUpdate,
  onSendRead,
  onSendDelete,
  onSendBulkInsert,
  onSendBulkUpdate,
  onMongoKeyDown,
}: MongoPanelProps) {
  return (
    <div style={styles.panel}>
      <h2 style={styles.panelTitle}>MongoDB</h2>
      <div style={styles.inputRow}>
        <input
          style={styles.input}
          type="text"
          placeholder="Enter message text..."
          value={mongoInputText}
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            setMongoInputText(e.target.value)
          }
          onKeyDown={onMongoKeyDown}
        />
      </div>
      <div style={styles.idRow}>
        <label style={styles.idLabel}>ID</label>
        <input
          style={styles.idInput}
          type="text"
          value={mongoId}
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            setMongoId(e.target.value || DEFAULT_MONGO_ID)
          }
          placeholder={DEFAULT_MONGO_ID}
          title="文件 _id（更新/讀取/刪除用），預設 trace_test"
        />
      </div>
      <div style={styles.buttonRow}>
        <button
          style={{ ...styles.button, ...styles.buttonTertiary }}
          onClick={onSendInsert}
          title="經 API 寫入 MongoDB（Insert），由 dbwatcher 監聽並轉發"
        >
          插入
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonMongo }}
          onClick={onSendUpdate}
          title="以指定 id 更新文件（Update），會更換 _oteltrace"
        >
          更新
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonMongo }}
          onClick={onSendRead}
          title="以指定 id 讀取文件（Read），span link 至文件內 _oteltrace"
        >
          讀取
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonMongo }}
          onClick={onSendDelete}
          title="以指定 id 刪除文件（Delete）"
        >
          刪除
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonTertiary }}
          onClick={onSendBulkInsert}
          title="BulkWrite：插入多筆（每筆帶 _oteltrace）"
        >
          Bulk 插入
        </button>
        <button
          style={{ ...styles.button, ...styles.buttonMongo }}
          onClick={onSendBulkUpdate}
          title="BulkWrite：依多組 id 更新（每筆帶 _oteltrace）"
        >
          Bulk 更新
        </button>
      </div>
      <div style={styles.traceFlow}>
        <span style={styles.traceFlowLabel}>Trace 經過：</span>
        <span style={styles.traceFlowPath}>
          Frontend → API → MongoDB → dbwatcher → NATS → Worker → WebSocket
        </span>
      </div>
    </div>
  );
}

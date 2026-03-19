# OTel Traces Test

**[English](README.md)**

---

一個展示 **OpenTelemetry 分散式追蹤（Distributed Tracing）** 的全端範例專案。使用者從瀏覽器送出訊息，可經多條路徑；**MongoDB** 路徑為：API 寫入 Mongo → **dbwatcher** 只發布 **文件 id** 至 NATS → **Worker 依 id 從 Mongo 讀出內容** 再 WebSocket 廣播。Worker 訂閱後透過 WebSocket 廣播回前端，**同一條 trace 從前端發送到前端接收** 貫穿各服務，可在 Grafana/Tempo 檢視完整路徑。

---

## 給首次使用者與 AI Agent 的專案摘要

- **目的**：示範端到端 W3C Trace Context 傳播：瀏覽器 → API（HTTP + NATS 發布）→ Worker（NATS 訂閱 + WebSocket 廣播）→ 瀏覽器（WebSocket 接收並建立最後一段 span）。
- **技術棧**：React 18 + TypeScript + Vite（前端，**Grafana Faro SDK** 負責 trace 與 W3C 傳播）、Go + Gin（API）、Go + net/http + otelhttp（Worker）、NATS（JetStream + Core）、OpenTelemetry（OTel）SDK、OTel Collector、Grafana Tempo、Grafana。
- **四條路徑**：**JetStream**（`POST /api/message` → `messages.new` → Worker）、**Core NATS**（`POST /api/message-core` → `messages.core` → Worker）、**Worker HTTP**（`POST /api/message-via-worker` → API 以 **otelresty** 呼叫 Worker `POST /notify`，HTTP 傳播 trace）、**MongoDB**（`POST /api/message-mongo` → Mongo → **dbwatcher** 發 id 至 `messages.db` → Worker **FindOne** → WebSocket）。可在 Tempo 依 trace ID 查詢。
- **Instrumentation**：`pkg/instrumentation-go`（Git submodule）— [instrumentation-go](https://github.com/Marz32onE/instrumentation-go)：**otel-nats**（otelnats + oteljetstream）、**otel-mongo**（otelmongo）、**otel-websocket**。各套件僅透過 option 接受 **TracerProvider** 與 **Propagators**，不提供 InitTracer；由應用程式在啟動時設定 global provider 與 propagator（見 **pkg/otelsetup** 與各套件 **example/**）。API 使用 [github.com/dubonzi/otelresty](https://github.com/dubonzi/otelresty) 作為 go-resty 的 OTel（span + trace 傳播）。
- **建置與執行**：Docker Compose 從專案根目錄 build；`api`、`worker`、`dbwatcher` 的 build context 為根目錄並複製 `pkg/instrumentation-go`。使用 `make up` 或 `docker compose up --build`（Makefile 預設為 `podman compose`，可覆寫 `COMPOSE_CMD='docker compose'`）。
- **關鍵設定**：`api`、`worker`、`dbwatcher` 皆需 `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` 才會把 span 送到 Tempo；前端透過 `VITE_OTEL_COLLECTOR_URL` 以 OTLP/HTTP 送 trace，經 Collector（CORS 支援）轉發到 Tempo。

---

## 架構總覽

```
┌──────────┐   HTTP POST    ┌──────────┐   NATS JetStream   ┌──────────┐
│          │  (traceparent) │          │   messages.new     │          │
│ Frontend ├───────────────►│   API    ├───────────────────►│  Worker  │
│ :3000    │                │  :8088   │   messages.db      │  :8082   │
│          │  /message-via- │     │    │  (trace context)  │     ▲    │
│          │  worker (HTTP)─┼─────┼────┼── POST /notify ───┼─────┘    │
│          │  /message-mongo│     ▼    │  (otelresty)       │ (net/http│
│          │                │ ┌────────┐   change stream   │ +otelhttp)│
│          │                │ │MongoDB │──► dbwatcher ──────┼─────┐    │
│          │                │ │ :27017 │   (→ messages.db) │     │    │
│          │◄─────────────────────────────────────────────────────────┤
│          │   WebSocket (JSON: traceparent, tracestate, body)         │
└────┬─────┘                                                └──────────┘
     │  OTLP/HTTP (browser)           OTLP/gRPC (api, worker, dbwatcher)
     ▼                                                      │
┌──────────────┐    OTLP/gRPC (to Tempo)   ◄───────────────┘
│     OTel     │
│  Collector   │──────────────────────────► ┌──────────┐
│ :4317 :4318  │                             │  Tempo   │
└──────────────┘                             │  :3200   │
                                             └────┬─────┘
                                             ┌────▼─────┐
                                             │ Grafana  │
                                             │  :3001   │
                                             └──────────┘
```

### 為什麼需要 OTel Collector？

API 與 Worker（Go）可直接用 gRPC 送 trace 到 Tempo。**瀏覽器** 則需透過 HTTP 送 OTLP，且 Tempo 的 OTLP receiver 不支援 CORS，瀏覽器會遭 CORS 阻擋。OTel Collector 的 HTTP receiver 支援 CORS，作為瀏覽器與 Tempo 之間的橋樑。

---

## 服務說明

| 服務               | 技術                            | 用途                                                                                                                             | Port                                   |
| ------------------ | ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------- |
| **Frontend**       | React 18 + TypeScript + Vite    | 兩區塊：NATS（JetStream/Core）、MongoDB（插入/更新/讀取/刪除，ID 可編輯）。最下方共用「由 WebSocket / Worker 監聽 NATS 取出的結果」顯示訊息；兩區塊各有 Trace 經過提示。 | `3000`                                 |
| **API**            | Go (Gin) + otelresty            | 端點：`POST /api/message`（JetStream）、`POST /api/message-core`（Core）、`POST /api/message-via-worker`（以 otelresty 呼叫 Worker）、`POST /api/message-mongo`（MongoDB）、`...-mongo-update/read/delete`；回傳 `trace_id`。環境變數：`WORKER_URL`（預設 `http://worker:8082`）。 | `8088`                                 |
| **Worker**         | Go (net/http + otelhttp)        | `GET /ws`（WebSocket）、`POST /notify`（HTTP，供 API/otelresty 呼叫）；JetStream + Core Subscribe；經 WebSocket 廣播 **traceparent/tracestate + body + api**。 | `8082`                                 |
| **MongoDB**        | Mongo 7 (replica set rs0)       | 儲存 `messaging.messages`；otelmongo 寫入時注入 `_oteltrace`。dbwatcher 監聽 change stream，需 replica set。                          | `27017`                                |
| **dbwatcher**      | Go + mongo-driver               | 監聽 **CRUD**；僅發布 **`{"op":"change","id":"..."}`** 或 delete（含 trace）；Worker 依 id **讀 Mongo** 後 WebSocket 廣播。 | 無對外 port                            |
| **NATS**           | NATS Alpine + JetStream         | 訊息佇列，持久化；healthcheck 需 `wget`（故用 alpine）                                                                           | `4222`（client）、`8222`（monitoring） |
| **OTel Collector** | OpenTelemetry Collector Contrib | 接收 gRPC/HTTP OTLP，轉發到 Tempo；HTTP 端點啟用 CORS 供瀏覽器使用                                                               | `4317`（gRPC）、`4318`（HTTP）         |
| **Tempo**          | Grafana Tempo 2.9.0             | 分散式追蹤後端（**鎖定 2.9.0**，v2.10+ 不相容）                                                                                  | `3200`                                 |
| **Grafana**        | Grafana latest                  | 視覺化介面，匿名 Admin、免登入；已配置 Tempo datasource                                                                          | `3001`                                 |

---

## 訊息流程

1. 使用者在 **NATS** 或 **MongoDB** 區塊輸入訊息，按下對應按鈕（JetStream、Core NATS，或 MongoDB 插入/更新/讀取/刪除）。MongoDB 操作使用可編輯的 **ID** 欄位（預設 `_id`；插入成功後會同步為回傳的 id）。
2. 前端建立對應的 send span（CLIENT），以 `traceparent` / `tracestate` header 傳播 context，對 API 發送 HTTP POST。
3. API 的 `otelgin` 延續同一 trace；**JetStream** 路徑使用 `oteljetstream.Publish`；**Core** 使用 `otelnats.Publish`；**MongoDB** 路徑使用 otelmongo Collection（Insert/Update/Read/Delete），回傳 `trace_id`。
4. **MongoDB 路徑**：dbwatcher 發布 **id 通知**（change/delete），trace 經文件 `_oteltrace` 寫入 NATS。Worker 對 **change** 以 **FindOne** 取 `text` 再廣播；delete 仍廣播 JSON。trace 鏈：API → Mongo → dbwatcher → NATS → Worker → Mongo 讀取 → WebSocket。
5. **JetStream / Core 路徑**：Worker 以 JetStream（Consume / Messages）或 Core Subscribe 收訊；**otelnats** / **oteljetstream** 從 headers 提取 trace context，建立 receive span，經 WebSocket 廣播。
6. 前端經 WebSocket 收訊；最下方 **共用結果區塊**「由 WebSocket / Worker 監聽 NATS 取出的結果」顯示所有訊息。若有 `traceparent` 與 `body`，則用 `propagation.extract` 還原 context，建立 `receive message` span（CONSUMER）。畫面上顯示 **Trace 驗證** 與 `trace_id`，可貼到 Grafana/Tempo 查詢。

---

## Trace 流程（Grafana 中可見）

一條完整 trace 的 span 樹範例：

**JetStream 路徑**（Core 路徑為 `messages.core` publish/receive）：

```
Frontend: send-message-jetstream          (SpanKind CLIENT)
  └─ API: POST /api/message               (otelgin, SpanKind SERVER)
       └─ API: messages.new publish       (jetstreamtrace, SpanKind PRODUCER)
            └─ Worker: messages.new receive (jetstreamtrace, SpanKind CONSUMER, messaging.consumer.name)
                 └─ Frontend: receive message (SpanKind CONSUMER)
```

**Worker HTTP 路徑**（otelresty）：

```
Frontend: send-message-via-worker-http    (SpanKind CLIENT)
  └─ API: POST /api/message-via-worker    (otelgin, SpanKind SERVER)
       └─ API: resty POST /notify         (otelresty, SpanKind CLIENT)
            └─ Worker: POST /notify       (otelhttp, SpanKind SERVER)
```

**MongoDB 路徑**（API 寫入 → dbwatcher 監聽 → 轉發 `messages.db` → Worker）：

```
Frontend: send-message-mongo              (SpanKind CLIENT)
  └─ API: POST /api/message-mongo         (otelgin, SpanKind SERVER)
       └─ API: MongoDB insert             (otelmongo)
            └─ dbwatcher: change stream 偵測 insert → Publish messages.db
                 └─ Worker: messages.db receive (SpanKind CONSUMER)
                      └─ Frontend: receive message (SpanKind CONSUMER)
```

- **Frontend**：OTLP/HTTP → OTel Collector（CORS 由 Collector 處理）。
- **API / Worker**：OTLP/gRPC → OTel Collector。
- **OTel Collector**：統一轉發到 Tempo。
- API 的 CORS 設定允許 `traceparent`、`tracestate`，確保瀏覽器能傳播 trace context。

---

## 前置需求

- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/)（或 Podman Compose，見下方 Makefile）
- [Git](https://git-scm.com/)（含 submodule 支援）

### 開發：測試與 Lint

- Go 單元測試使用 **testify**，位於 `pkg/instrumentation-go`（otel-nats、otel-mongo）及 `api`/`worker`/`dbwatcher`。
- 建議於各模組執行 **`go vet ./...`** 與 **`go test ./...`**（如 `api`、`worker`、`dbwatcher`、`pkg/otelsetup`、`pkg/instrumentation-go/otel-nats`、`pkg/instrumentation-go/otel-mongo`、`pkg/instrumentation-go/otel-websocket`）。
- 若已安裝 [golangci-lint](https://golangci-lint.run/)，可於專案根目錄執行 **`golangci-lint run ./...`** 進行靜態檢查（需在各 Go 模組目錄分別執行，或使用根目錄的 `go work` / 依序 `cd` 各模組）。

---

## 快速開始

```bash
# Clone（含 submodule）
git clone --recurse-submodules git@github.com:Marz32onE/otel-traces-test.git
cd otel-traces-test

# 若已 clone 但未拉 submodule
git submodule update --init

# 啟動所有服務（建議）
make up
# 若要用 docker compose：
# COMPOSE_CMD='docker compose' make up
# 或直接：
# docker compose up --build
```

啟動後可開啟：

| 服務            | URL                   |
| --------------- | --------------------- |
| Frontend        | http://localhost:3000 |
| Grafana         | http://localhost:3001 |
| NATS Monitoring | http://localhost:8222 |
| Tempo API       | http://localhost:3200 |

### 查看 Traces

1. 開啟 http://localhost:3001（Grafana，免登入）。
2. 左側選 **Explore**，資料來源選 **Tempo**。
3. 搜尋模式選 **Search**，Service Name 選 `frontend`、`api` 或 `worker`。
4. 點選任一 trace，可看到從 `send-message` 到 `receive message` 的完整路徑。

### 驗證 Trace（命令列）

API 回傳 `trace_id`，可用於確認同一 trace 貫穿 API 與 Worker：

```bash
# JetStream
RES=$(curl -s -X POST http://localhost:8088/api/message \
  -H "Content-Type: application/json" \
  -d '{"text":"hello jetstream"}')
echo "$RES"   # {"endpoint":"JetStream","status":"published","trace_id":"..."}

# Core NATS
RES=$(curl -s -X POST http://localhost:8088/api/message-core \
  -H "Content-Type: application/json" \
  -d '{"text":"hello core"}')
echo "$RES"   # {"endpoint":"Core","status":"published","trace_id":"..."}

# 數秒後以 trace_id 查 Tempo（將 <TRACE_ID> 換成上方的 trace_id）
sleep 5
curl -s "http://localhost:3200/api/traces/<TRACE_ID>" | head -c 500
# 應可看到 service.name 為 api 與 worker 的 spans（例如 POST /api/message、messages.new publish、messages.new receive）
```

若要看到 **含前端的完整 trace**，請從瀏覽器 http://localhost:3000 送一則訊息；畫面上會顯示該次請求的 **Trace 驗證** 與 `trace_id`，可複製到 Grafana Explore → Tempo 查詢。

### 驗證完整路徑（含 MongoDB）

專案根目錄提供腳本一次驗證 **Go 建置、Docker 啟動、所有 API 端點、MongoDB 路徑（API → Mongo → dbwatcher → NATS → Worker）、前端與 Tempo trace**：

```bash
./scripts/verify-full-path.sh
```

需已安裝 `go`，以及任一 compose runtime：`docker compose`、`podman compose` 或 `podman-compose`。腳本會自動偵測 compose 指令：建置 api / worker / dbwatcher、`up -d --build`、等待服務就緒、對 `/api/message`、`/api/message-core`、`/api/message-mongo` 發送請求、檢查 dbwatcher/worker 日誌是否出現轉發與接收、並以 traceparent 呼叫 `/api/message-mongo` 後查詢 Tempo。

手動驗證 MongoDB 路徑可依序執行：

```bash
# 寫入 MongoDB（API）
curl -s -X POST http://localhost:8088/api/message-mongo -H "Content-Type: application/json" -d '{"text":"mongo-e2e"}'
# 預期: {"endpoint":"MongoDB","status":"stored","trace_id":"..."}

# 數秒後檢查 dbwatcher 是否轉發、worker 是否收到
make logs SVC=dbwatcher   # 應見 "Forwarded to messages.db"
make logs SVC=worker       # 應見 "[DB] received"
```

### 停止與清理

```bash
make down
make clean   # 含 volumes

# 若你使用 Docker，也可直接：
# docker compose down
# docker compose down -v
```

---

## 專案結構

```
.
├── api/                    # Go + Gin API（端點、trace_id）
├── worker/                 # Go net/http + otelhttp（WebSocket + POST /notify）
├── dbwatcher/              # Mongo change stream → NATS
├── frontend/               # React + Vite + Grafana Faro
├── pkg/
│   ├── instrumentation-go/   # Git submodule — otel-nats、otel-mongo、otel-websocket
│   │   ├── otel-nats/          # otelnats、oteljetstream（NATS + JetStream OTel）
│   │   ├── otel-mongo/        # otelmongo（MongoDB OTel，_oteltrace）
│   │   ├── otel-websocket/    # WebSocket trace 傳播
│   │   └── example/           # （各套件下）如何 init TracerProvider + 使用套件
│   └── otelsetup/             # 共用 OTLP TracerProvider 初始化（Init、Shutdown）
├── charts/otel-traces-test/config/
├── grafana/
├── docker-compose.yml
├── Makefile
└── LICENSE
```

`api`、`worker`、`dbwatcher` 的 Dockerfile 以專案根目錄為 build context，並複製 `pkg/instrumentation-go`。

### Tracing：Provider / Propagator 初始化（符合 OTel Go Contrib）

Instrumentation 套件（**otelnats**、**oteljetstream**、**otelmongo**、**otelwebsocket**）**不提供** `InitTracer`，僅透過 option 接受 **TracerProvider** 與 **Propagators**，未設定時使用 `otel.GetTracerProvider()` / `otel.GetTextMapPropagator()`。

- **本專案**：`api`、`worker`、`dbwatcher` 使用 **pkg/otelsetup**：在啟動時呼叫 **`otelsetup.Init("", attribute.String("service.name", "api"), ...)`** 一次，接著 **`defer otelsetup.Shutdown(tp)`**。之後直接使用 `otelnats.Connect`、`otelmongo.NewClient` 等，無需各套件單獨 init。
- **範例**：`pkg/instrumentation-go` 下各套件皆有 **example/** 目錄，示範如何建立 OTLP TracerProvider、設定 `otel.SetTracerProvider` 與 `otel.SetTextMapPropagator`，再使用該套件。

---

## Git Submodules

| 路徑                      | 說明 |
|---------------------------|------|
| `pkg/instrumentation-go` | [instrumentation-go](https://github.com/Marz32onE/instrumentation-go) — otel-nats（otelnats + oteljetstream）、otel-mongo（otelmongo）、otel-websocket。分支：`feat/trace-propagation-mod`。 |

**依賴（非 submodule）**：[dubonzi/otelresty](https://github.com/dubonzi/otelresty) — go-resty 的 OTel；API 用以外呼 Worker（spans + trace 傳播）。

Clone 後執行 `git submodule update --init` 以取得 `pkg/instrumentation-go`。

---

## 環境變數

### API

| 變數                          | 預設值                  | 說明                                                          |
| ----------------------------- | ----------------------- | ------------------------------------------------------------- |
| `NATS_URL`                    | `nats://localhost:4222` | NATS 連線地址                                                 |
| `WORKER_URL`                  | `http://worker:8082`    | Worker 基底 URL（`/api/message-via-worker` 經 otelresty 呼叫用） |
| `MONGODB_URI`                 | `mongodb://localhost:27017` | MongoDB 連線地址（`/api/message-mongo` 寫入用）            |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | OTel Collector gRPC 端點（Docker 內為 `otel-collector:4317`） |

### Worker

| 變數                          | 預設值                  | 說明                                                                                                       |
| ----------------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------- |
| `NATS_URL`                    | `nats://localhost:4222` | NATS 連線地址                                                                                              |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | OTel Collector gRPC 端點（**必設**，否則 worker span 不會出現在 Tempo；Docker 內為 `otel-collector:4317`） |

### dbwatcher

| 變數                          | 預設值                  | 說明                                                                 |
| ----------------------------- | ----------------------- | -------------------------------------------------------------------- |
| `MONGODB_URI`                 | `mongodb://localhost:27017` | MongoDB 連線地址（監聽 `messaging.messages` change stream）      |
| `NATS_URL`                    | `nats://localhost:4222` | NATS 連線地址（發布至 JetStream 主題 `messages.db`）                 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | OTel Collector gRPC 端點（Docker 內為 `otel-collector:4317`）        |

### Frontend（Build-time）

| 變數                      | 預設值                  | 說明                                       |
| ------------------------- | ----------------------- | ------------------------------------------ |
| `VITE_API_URL`            | `http://localhost:8088` | API server URL                             |
| `VITE_WS_URL`             | `ws://localhost:8082`   | WebSocket worker URL                       |
| `VITE_OTEL_COLLECTOR_URL` | `http://localhost:4318` | OTel Collector HTTP 端點（瀏覽器 OTLP 用） |

---

## 疑難排解

### 重啟 OTel Collector 後 API traces 消失

重啟 `otel-collector` 後，API 可能出現 `dial tcp ...: no route to host`（Docker 網路變更）。一併重啟依賴服務：

```bash
make restart
# 若要用 docker compose：
# COMPOSE_CMD='docker compose' make restart
```

### Worker 的 span 沒有出現在 Tempo

確認 worker 有設定 `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317`（見 `docker-compose.yml`）。未設定時 worker 預設連 `localhost:4317`，在容器內無法連到 Collector。

### Docker build 快取導致前端更新未生效

強制無快取重建：

```bash
make build && make up
# 若要用 docker compose：
# COMPOSE_CMD='docker compose' make build && COMPOSE_CMD='docker compose' make up
```

### Tempo 查詢 trace 回傳 404

Tempo 需數秒將 span 寫入可查詢的 block。送完訊息後稍等再查：

```bash
sleep 5
curl -s -G "http://localhost:3200/api/search" --data-urlencode "tags=service.name=api"
```

### Go 服務的 Dockerfile build context

`api`、`worker`、`dbwatcher` 的 Dockerfile 以專案根目錄為 build context，並 `COPY pkg/instrumentation-go ./pkg/instrumentation-go/`；各服務的 `go.mod` 使用 `replace` 指向本地 `pkg/instrumentation-go` 與 `pkg/otelsetup`。

### Makefile 使用 Docker 而非 Podman

```bash
COMPOSE_CMD='docker compose' make up
```

---

## License

[Apache License 2.0](LICENSE)

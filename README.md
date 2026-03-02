# OTel Traces Test

一個展示 **OpenTelemetry 分散式追蹤（Distributed Tracing）** 的全端範例專案。使用者從瀏覽器送出訊息，經過 API 發布到 NATS JetStream，Worker 訂閱後透過 WebSocket 廣播回前端；**同一條 trace 從前端發送到前端接收** 貫穿 API 與 Worker，可在 Grafana/Tempo 檢視完整路徑。

---

## 給首次使用者與 AI Agent 的專案摘要

- **目的**：示範端到端 W3C Trace Context 傳播：瀏覽器 → API（HTTP + NATS 發布）→ Worker（NATS 訂閱 + WebSocket 廣播）→ 瀏覽器（WebSocket 接收並建立最後一段 span）。
- **技術棧**：React 18 + TypeScript + Vite（前端）、Go + Gin（API）、Go + gorilla/websocket（Worker）、NATS JetStream、OpenTelemetry（OTel）SDK、OTel Collector、Grafana Tempo、Grafana。
- **Trace 路徑**：Frontend `send-message` → API `POST /api/message` → API `messages.new publish` → Worker `messages.new receive` → Frontend `receive message`（同一 trace ID）。
- **Submodules**：`pkg/nats.go`（NATS Go client）、`pkg/natstrace`（本專案使用的 NATS OTel 包裝，W3C 傳播 + JetStream）。Clone 後需 `git submodule update --init`。
- **建置與執行**：Docker Compose 從專案根目錄 build；`api` 與 `worker` 的 build context 為根目錄（需存取 `pkg/`）。使用 `docker compose up --build` 或 `make up`（Makefile 預設為 `podman compose`，可覆寫 `COMPOSE_CMD=docker compose`）。
- **關鍵設定**：`api`、`worker` 皆需 `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` 才會把 span 送到 Tempo；前端透過 `VITE_OTEL_COLLECTOR_URL` 以 OTLP/HTTP 送 trace，經 Collector（CORS 支援）轉發到 Tempo。

---

## 架構總覽

```
┌──────────┐   HTTP POST    ┌──────────┐   NATS JetStream   ┌──────────┐
│          │  (traceparent) │          │   messages.new     │          │
│ Frontend ├───────────────►│   API    ├───────────────────►│  Worker  │
│ :3000    │                │  :8081   │  (headers carry   │  :8082   │
│          │                │          │   trace context) │          │
│          │◄───────────────────────────────────────────────┤          │
│          │   WebSocket (JSON: traceparent, tracestate, body)           │
└────┬─────┘                                                └──────────┘
     │
     │  OTLP/HTTP (browser)                    OTLP/gRPC (api, worker)
     ▼                                                      │
┌──────────────┐    OTLP/gRPC (to Tempo)   ◄────────────────┘
│     OTel     │
│  Collector   │
│ :4317 :4318  │──────────────────────────► ┌──────────┐
└──────────────┘                             │  Tempo   │
                                             │  :3200   │
                                             └────┬─────┘
                                                  │
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
| **Frontend**       | React 18 + TypeScript + Vite    | 使用者介面：輸入訊息、即時接收 WebSocket；建立 `send-message` 與 `receive message` span                                          | `3000`                                 |
| **API**            | Go (Gin)                        | 接收 HTTP POST，以 `natstrace` 發布到 NATS JetStream（subject: `messages.new`）                                                  | `8081`                                 |
| **Worker**         | Go (gorilla/websocket)          | 訂閱 NATS `messages.new`，收到後將 **traceparent/tracestate + body** 以 JSON 經 WebSocket 廣播；建立 `messages.new receive` span | `8082`                                 |
| **NATS**           | NATS Alpine + JetStream         | 訊息佇列，持久化；healthcheck 需 `wget`（故用 alpine）                                                                           | `4222`（client）、`8222`（monitoring） |
| **OTel Collector** | OpenTelemetry Collector Contrib | 接收 gRPC/HTTP OTLP，轉發到 Tempo；HTTP 端點啟用 CORS 供瀏覽器使用                                                               | `4317`（gRPC）、`4318`（HTTP）         |
| **Tempo**          | Grafana Tempo 2.9.0             | 分散式追蹤後端（**鎖定 2.9.0**，v2.10+ 不相容）                                                                                  | `3200`                                 |
| **Grafana**        | Grafana latest                  | 視覺化介面，匿名 Admin、免登入；已配置 Tempo datasource                                                                          | `3001`                                 |

---

## 訊息流程

1. 使用者在前端輸入訊息，按下 **Send**（或 Enter）。
2. 前端建立 `send-message` span（CLIENT），並以 `traceparent` / `tracestate` header 傳播 context，對 API 發送 HTTP POST。
3. API 的 `otelgin` middleware 延續同一 trace，處理 `POST /api/message`；API 使用 `natstrace` 的 `PublishJetStream` 建立 `messages.new publish` span，並將 trace context 注入 NATS 訊息 **headers**，發布到 JetStream。
4. Worker 以 `SubscribeJetStream("messages.new", ...)` 訂閱；`natstrace` 從訊息 headers 提取 trace context，建立 `messages.new receive` span，並在該 context 下將 **traceparent、tracestate、body** 組成 JSON 經 WebSocket 廣播。
5. 前端收到 WebSocket 訊息後解析 JSON；若有 `traceparent` 與 `body`，則用 `propagation.extract` 還原 context，在該 context 下建立 `receive message` span（CONSUMER），完成同一條 trace。

---

## Trace 流程（Grafana 中可見）

一條完整 trace 的 span 樹範例（從前端送出一則訊息並經 WebSocket 收回時）：

```
Frontend: send-message                    (SpanKind CLIENT, message.content)
  └─ API: POST /api/message               (otelgin, SpanKind SERVER)
       └─ API: messages.new publish       (natstrace, SpanKind PRODUCER)
            └─ Worker: messages.new receive (natstrace, SpanKind CONSUMER)
                 └─ Frontend: receive message (SpanKind CONSUMER, message.content)
```

- **Frontend**：OTLP/HTTP → OTel Collector（CORS 由 Collector 處理）。
- **API / Worker**：OTLP/gRPC → OTel Collector。
- **OTel Collector**：統一轉發到 Tempo。
- API 的 CORS 設定允許 `traceparent`、`tracestate`，確保瀏覽器能傳播 trace context。

---

## 前置需求

- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/)（或 Podman Compose，見下方 Makefile）
- [Git](https://git-scm.com/)（含 submodule 支援）

---

## 快速開始

```bash
# Clone（含 submodule）
git clone --recurse-submodules git@github.com:Marz32onE/otel-traces-test.git
cd otel-traces-test

# 若已 clone 但未拉 submodule
git submodule update --init

# 啟動所有服務（二擇一）
docker compose up --build
# 或使用 Makefile（預設為 podman compose）
make up
# 若要用 docker compose： COMPOSE_CMD='docker compose' make up
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

僅用 curl 時，trace 起點在 API（無前端 span），但仍可確認 API → Worker 同 trace：

```bash
curl -X POST http://localhost:8081/api/message \
  -H "Content-Type: application/json" \
  -d '{"text":"hello trace"}'

# 數秒後查 Tempo（以實際回傳的 trace ID 替換）
curl -s -G "http://localhost:3200/api/search" --data-urlencode "tags=service.name=api"
# 或依 trace ID： curl http://localhost:3200/api/traces/<traceID>
```

若要看到 **含前端的完整 trace**，請從瀏覽器 http://localhost:3000 送一則訊息，再於 Grafana Explore 查 `frontend` 或 `api` 的最近 trace。

### 停止與清理

```bash
docker compose down
# 或連同 volumes 一併刪除
docker compose down -v

# 使用 Makefile 時
make down
make clean   # 含 -v
```

---

## 專案結構

```
.
├── api/                        # API server（Go + Gin）
│   ├── Dockerfile              # 從專案根目錄 build（需 pkg/nats.go、pkg/natstrace）
│   ├── go.mod                  # replace => ../pkg/nats.go, ../pkg/natstrace
│   ├── go.sum
│   └── main.go                 # OTel + otelgin + natstrace PublishJetStream
├── worker/                     # WebSocket worker（Go）
│   ├── Dockerfile              # 從專案根目錄 build（同上）
│   ├── go.mod
│   ├── go.sum
│   └── main.go                 # natstrace SubscribeJetStream + WS broadcast（含 traceparent）
├── frontend/                   # React 前端（TypeScript）
│   ├── Dockerfile
│   ├── package.json, tsconfig.json, vite.config.js, index.html, nginx.conf
│   └── src/
│       ├── main.tsx
│       ├── App.tsx             # UI、send-message / receive message span、WS 解析 traceparent
│       └── tracing.ts          # OTel 初始化（WebTracerProvider、OTLP HTTP export、W3C propagator）
├── pkg/
│   ├── nats.go/                # Git submodule — NATS Go client（nats.io 上游）
│   └── natstrace/          # Git submodule — NATS OTel 包裝（W3C 傳播、Publish/Subscribe/JetStream）
├── nats/
│   └── nats-server.conf
├── grafana/
│   └── provisioning/datasources/datasource.yml   # Tempo datasource
├── docker-compose.yml
├── otel-collector-config.yaml  # OTLP gRPC/HTTP receiver、CORS、export 到 Tempo
├── tempo.yaml                  # Tempo 2.9.0 設定（local storage）
├── Makefile                    # up / down / clean / build / logs / ps（預設 COMPOSE_CMD=podman compose）
└── LICENSE                     # Apache 2.0
```

`api/Dockerfile` 與 `worker/Dockerfile` 的 build context 為專案根目錄，因 `go.mod` 的 `replace` 需存取 `pkg/nats.go` 與 `pkg/natstrace`。

---

## Git Submodules

本專案使用兩個 submodule：

| 路徑            | 說明                                                                    | 備註                                                                                        |
| --------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `pkg/nats.go`   | [NATS Go client](https://github.com/nats-io/nats.go)（可為 fork）       | `api`、`worker` 的 `go.mod` 以 `replace` 指向 `../pkg/nats.go`                              |
| `pkg/natstrace` | [natstrace](https://github.com/Marz32onE/natstrace) — NATS 的 OTel 包裝 | W3C Trace Context 注入/提取、Publish/Subscribe/JetStream；`replace` 指向 `../pkg/natstrace` |

`api` 與 `worker` 的 `go.mod` 範例：

```
replace (
	github.com/Marz32onE/natstrace => ../pkg/natstrace
	github.com/nats-io/nats.go => ../pkg/nats.go
)
```

修改 `pkg/nats.go` 或 `pkg/natstrace` 後，在專案根目錄重建 api/worker 映像即可使用最新程式碼。

---

## 環境變數

### API

| 變數                          | 預設值                  | 說明                                                          |
| ----------------------------- | ----------------------- | ------------------------------------------------------------- |
| `NATS_URL`                    | `nats://localhost:4222` | NATS 連線地址                                                 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | OTel Collector gRPC 端點（Docker 內為 `otel-collector:4317`） |

### Worker

| 變數                          | 預設值                  | 說明                                                                                                       |
| ----------------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------- |
| `NATS_URL`                    | `nats://localhost:4222` | NATS 連線地址                                                                                              |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | OTel Collector gRPC 端點（**必設**，否則 worker span 不會出現在 Tempo；Docker 內為 `otel-collector:4317`） |

### Frontend（Build-time）

| 變數                      | 預設值                  | 說明                                       |
| ------------------------- | ----------------------- | ------------------------------------------ |
| `VITE_API_URL`            | `http://localhost:8081` | API server URL                             |
| `VITE_WS_URL`             | `ws://localhost:8082`   | WebSocket worker URL                       |
| `VITE_OTEL_COLLECTOR_URL` | `http://localhost:4318` | OTel Collector HTTP 端點（瀏覽器 OTLP 用） |

---

## 疑難排解

### 重啟 OTel Collector 後 API traces 消失

重啟 `otel-collector` 後，API 可能出現 `dial tcp ...: no route to host`（Docker 網路變更）。一併重啟依賴服務：

```bash
docker compose restart otel-collector api worker
```

### Worker 的 span 沒有出現在 Tempo

確認 worker 有設定 `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317`（見 `docker-compose.yml`）。未設定時 worker 預設連 `localhost:4317`，在容器內無法連到 Collector。

### Docker build 快取導致前端更新未生效

強制無快取重建：

```bash
docker compose build --no-cache frontend
docker compose up -d frontend
```

### Tempo 查詢 trace 回傳 404

Tempo 需數秒將 span 寫入可查詢的 block。送完訊息後稍等再查：

```bash
sleep 5
curl -s -G "http://localhost:3200/api/search" --data-urlencode "tags=service.name=api"
```

### Go 服務的 Dockerfile build context

`api/Dockerfile` 與 `worker/Dockerfile` 的 build context 為專案根目錄；`COPY` 路徑相對於根目錄（例如 `COPY api/go.mod api/go.sum ./api/`、`COPY pkg/natstrace/ ./pkg/natstrace/`）。

### Makefile 使用 Docker 而非 Podman

```bash
COMPOSE_CMD='docker compose' make up
```

---

## License

[Apache License 2.0](LICENSE)

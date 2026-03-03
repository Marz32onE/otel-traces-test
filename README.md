# OTel Traces Test

一個展示 **OpenTelemetry 分散式追蹤（Distributed Tracing）** 的全端範例專案。使用者從瀏覽器送出訊息，經過 API 發布到 NATS JetStream，Worker 訂閱後透過 WebSocket 廣播回前端；**同一條 trace 從前端發送到前端接收** 貫穿 API 與 Worker，可在 Grafana/Tempo 檢視完整路徑。

---

## 給首次使用者與 AI Agent 的專案摘要

- **目的**：示範端到端 W3C Trace Context 傳播：瀏覽器 → API（HTTP + NATS 發布）→ Worker（NATS 訂閱 + WebSocket 廣播）→ 瀏覽器（WebSocket 接收並建立最後一段 span）。
- **技術棧**：React 18 + TypeScript + Vite（前端，**Grafana Faro SDK** 負責 trace 與 W3C 傳播）、Go + Gin（API）、Go + gorilla/websocket（Worker）、NATS（JetStream + Core）、OpenTelemetry（OTel）SDK、OTel Collector、Grafana Tempo、Grafana。
- **兩條路徑**：**JetStream**（`POST /api/message` → `messages.new` → Worker Consume/Messages）與 **Core NATS**（`POST /api/message-core` → `messages.core` → Worker Subscribe）；同一 trace 從 API 貫穿到 Worker，可在 Tempo 依 trace ID 查詢。
- **Submodule**：`pkg/natstrace`（[natstrace](https://github.com/Marz32onE/natstrace) — NATS 的 OTel 包裝，W3C 傳播 + JetStream/Core）。Clone 後需 `git submodule update --init`。
- **建置與執行**：Docker Compose 從專案根目錄 build；`api` 與 `worker` 的 build context 為根目錄。使用 `make up` 或 `docker compose up --build`（Makefile 預設為 `podman compose`，可覆寫 `COMPOSE_CMD='docker compose'`）。
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
| **API**            | Go (Gin)                        | 兩端點：`POST /api/message`（JetStream `messages.new`）、`POST /api/message-core`（Core `messages.core`）；回傳 `trace_id` 供 Tempo 查詢 | `8081`                                 |
| **Worker**         | Go (gorilla/websocket)          | JetStream（Consume + Messages）與 Core Subscribe；收到後將 **traceparent/tracestate + body + api** 以 JSON 經 WebSocket 廣播 | `8082`                                 |
| **NATS**           | NATS Alpine + JetStream         | 訊息佇列，持久化；healthcheck 需 `wget`（故用 alpine）                                                                           | `4222`（client）、`8222`（monitoring） |
| **OTel Collector** | OpenTelemetry Collector Contrib | 接收 gRPC/HTTP OTLP，轉發到 Tempo；HTTP 端點啟用 CORS 供瀏覽器使用                                                               | `4317`（gRPC）、`4318`（HTTP）         |
| **Tempo**          | Grafana Tempo 2.9.0             | 分散式追蹤後端（**鎖定 2.9.0**，v2.10+ 不相容）                                                                                  | `3200`                                 |
| **Grafana**        | Grafana latest                  | 視覺化介面，匿名 Admin、免登入；已配置 Tempo datasource                                                                          | `3001`                                 |

---

## 訊息流程

1. 使用者在前端輸入訊息，按下 **送出（JetStream）** 或 **送出（Core NATS fire-and-go）**（或 Enter 對應 JetStream）。
2. 前端建立 `send-message-jetstream` / `send-message-core` span（CLIENT），以 `traceparent` / `tracestate` header 傳播 context，對 API 發送 HTTP POST。
3. API 的 `otelgin` 延續同一 trace；**JetStream** 路徑使用 `jetstreamtrace.Publish` 建立 `messages.new publish` span，**Core** 路徑使用 `natstrace.Publish` 建立 `messages.core publish`；trace context 注入 NATS 訊息 headers。
4. Worker 以 JetStream（Consume / Messages）或 Core Subscribe 收訊；`natstrace` / `jetstreamtrace` 從 headers 提取 trace context，建立 receive span（JetStream 帶 `messaging.consumer.name`），並將 **traceparent、tracestate、body、api** 組成 JSON 經 WebSocket 廣播。
5. 前端收到 WebSocket 訊息後解析 JSON；若有 `traceparent` 與 `body`，則用 `propagation.extract` 還原 context，建立 `receive message` span（CONSUMER），完成同一條 trace。畫面上會顯示 **Trace 驗證** 與上次送出的 `trace_id`，可貼到 Grafana/Tempo 查詢。

---

## Trace 流程（Grafana 中可見）

一條完整 trace 的 span 樹範例（JetStream 路徑；Core 路徑為 `messages.core` publish/receive）：

```
Frontend: send-message-jetstream          (SpanKind CLIENT)
  └─ API: POST /api/message               (otelgin, SpanKind SERVER)
       └─ API: messages.new publish       (jetstreamtrace, SpanKind PRODUCER)
            └─ Worker: messages.new receive (jetstreamtrace, SpanKind CONSUMER, messaging.consumer.name)
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

API 回傳 `trace_id`，可用於確認同一 trace 貫穿 API 與 Worker：

```bash
# JetStream
RES=$(curl -s -X POST http://localhost:8081/api/message \
  -H "Content-Type: application/json" \
  -d '{"text":"hello jetstream"}')
echo "$RES"   # {"endpoint":"JetStream","status":"published","trace_id":"..."}

# Core NATS
RES=$(curl -s -X POST http://localhost:8081/api/message-core \
  -H "Content-Type: application/json" \
  -d '{"text":"hello core"}')
echo "$RES"   # {"endpoint":"Core","status":"published","trace_id":"..."}

# 數秒後以 trace_id 查 Tempo（將 <TRACE_ID> 換成上方的 trace_id）
sleep 5
curl -s "http://localhost:3200/api/traces/<TRACE_ID>" | head -c 500
# 應可看到 service.name 為 api 與 worker 的 spans（例如 POST /api/message、messages.new publish、messages.new receive）
```

若要看到 **含前端的完整 trace**，請從瀏覽器 http://localhost:3000 送一則訊息；畫面上會顯示該次請求的 **Trace 驗證** 與 `trace_id`，可複製到 Grafana Explore → Tempo 查詢。

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
│   ├── Dockerfile              # 從專案根目錄 build
│   ├── go.mod                  # require natstrace v0.1.4，replace => ../pkg/natstrace
│   ├── go.sum
│   └── main.go                 # OTel + otelgin + natstrace/jetstreamtrace（兩端點、回傳 trace_id）
├── worker/                     # WebSocket worker（Go）
│   ├── Dockerfile              # 從專案根目錄 build
│   ├── go.mod
│   ├── go.sum
│   └── main.go                 # natstrace/jetstreamtrace 訂閱 + WS broadcast（traceparent、body、api）
├── frontend/                   # React 前端（TypeScript）
│   ├── Dockerfile
│   ├── package.json, tsconfig.json, vite.config.js, index.html, nginx.conf
│   └── src/
│       ├── main.tsx
│       ├── App.tsx             # 雙按鈕（JetStream / Core）、Trace 驗證顯示 trace_id、WS 解析 traceparent
│       └── tracing.ts          # Grafana Faro 初始化（TracingInstrumentation、OtlpHttpTransport、propagateTraceHeaderCorsUrls）
├── pkg/
│   └── natstrace/              # Git submodule — natstrace（NATS OTel 包裝，W3C + JetStream/Core）
├── charts/otel-traces-test/config/   # tempo.yaml、otel-collector.yaml（Compose 掛載）
├── grafana/
│   └── provisioning/datasources/datasource.yml   # Tempo datasource
├── docker-compose.yml
├── Makefile                    # up / down / clean / build / logs / ps（預設 COMPOSE_CMD=podman compose）
└── LICENSE                     # Apache 2.0
```

`api/Dockerfile` 與 `worker/Dockerfile` 的 build context 為專案根目錄。

---

## Git Submodules

本專案使用一個 submodule：

| 路徑            | 說明                                                                 | 備註                                           |
| --------------- | -------------------------------------------------------------------- | ---------------------------------------------- |
| `pkg/natstrace` | [natstrace](https://github.com/Marz32onE/natstrace) — NATS 的 OTel 包裝 | W3C 傳播、JetStream/Core；建議使用 tag v0.1.4+ |

`api` 與 `worker` 的 `go.mod` 使用 `replace` 指向本地 submodule（本地開發時）：

```
replace github.com/Marz32onE/natstrace => ../pkg/natstrace
```

Clone 後執行 `git submodule update --init`；若要指定版本可 `cd pkg/natstrace && git fetch --tags && git checkout v0.1.4`。修改 `pkg/natstrace` 後，在專案根目錄重建 api/worker 映像即可使用最新程式碼。

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

`api/Dockerfile` 與 `worker/Dockerfile` 的 build context 為專案根目錄；`COPY` 路徑相對於根目錄。若使用 `replace github.com/Marz32onE/natstrace => ../pkg/natstrace`，Docker build 時需能取得 `pkg/natstrace`（例如 context 含 submodule，或 Dockerfile 內 `COPY pkg/natstrace ./pkg/natstrace/`）；否則改為依賴遠端版本（移除 replace、`go get` 可取得 v0.1.4+）。

### Makefile 使用 Docker 而非 Podman

```bash
COMPOSE_CMD='docker compose' make up
```

---

## License

[Apache License 2.0](LICENSE)

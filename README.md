# OTel Traces Test

一個展示 **OpenTelemetry 分散式追蹤（Distributed Tracing）** 的全端範例專案。使用者從瀏覽器送出訊息，經過 API server 發布到 NATS JetStream，再透過 WebSocket worker 即時推送回前端，全程可在 Grafana 中查看完整的 trace。

## 架構總覽

```
┌──────────┐   HTTP POST    ┌──────────┐   NATS JetStream   ┌──────────┐
│          │  (traceparent) │          │   messages.new     │          │
│ Frontend ├───────────────►│   API    ├───────────────────►│  Worker  │
│ :3000    │                │  :8081   │                    │  :8082   │
│          │◄───────────────────────────────────────────────┤          │
└────┬─────┘   WebSocket                                   └──────────┘
     │
     │  OTLP/HTTP                        OTLP/gRPC
     ▼                                       │
┌──────────────┐    OTLP/gRPC    ┌───────────┘
│     OTel     │◄────────────────┘
│  Collector   ├──────────────────►┌──────────┐
│ :4317 :4318  │    OTLP/gRPC     │  Tempo   │
└──────────────┘                   │  :3200   │
                                   └────┬─────┘
                                        │
                                   ┌────▼─────┐
                                   │ Grafana  │
                                   │  :3001   │
                                   └──────────┘
```

## 服務說明

| 服務 | 技術 | 用途 | Port |
|---|---|---|---|
| **Frontend** | React 18 + TypeScript + Vite | 使用者介面，輸入訊息並即時接收 | `3000` |
| **API** | Go (Gin) | 接收 HTTP 請求，發布訊息到 NATS | `8081` |
| **Worker** | Go (gorilla/websocket) | 訂閱 NATS 訊息，透過 WebSocket 廣播給前端 | `8082` |
| **NATS** | NATS 2.10 + JetStream | 訊息佇列，持久化儲存 | `4222` |
| **OTel Collector** | OpenTelemetry Collector Contrib | 收集 traces（瀏覽器 HTTP + API gRPC），轉發到 Tempo | `4317` `4318` |
| **Tempo** | Grafana Tempo | 分散式追蹤後端，儲存與查詢 traces | `3200` |
| **Grafana** | Grafana | 視覺化介面，查看 traces | `3001` |

## 訊息流程

1. 使用者在前端輸入訊息，按下 **Send**
2. 前端建立一個 `send-message` span（包含訊息內容），透過 `traceparent` header 傳播 trace context
3. API server 接收請求，`otelgin` middleware 自動延續同一條 trace
4. API 建立 `publish-to-nats` 子 span，將訊息發布到 NATS JetStream（subject: `messages.new`）
5. Worker 訂閱 `messages.new`，收到訊息後透過 WebSocket 廣播給所有連線的前端

## Trace 流程

在 Grafana 中可看到一條完整的 trace，包含：

```
Frontend: send-message              (message.content = "hello")
  └─ API: POST /api/message         (otelgin auto-instrumentation)
       └─ API: publish-to-nats      (nats.subject = "messages.new")
```

- **Frontend** 透過 OTLP/HTTP 將 span 送到 OTel Collector
- **API** 透過 OTLP/gRPC 將 span 送到 OTel Collector
- **OTel Collector** 統一轉發到 Tempo

## 前置需求

- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/)
- [Git](https://git-scm.com/)（含 submodule 支援）

## 快速開始

```bash
# Clone（含 submodule）
git clone --recurse-submodules git@github.com:Marz32onE/otel-traces-test.git
cd otel-traces-test

# 如果已經 clone 但忘了拉 submodule
git submodule update --init

# 啟動所有服務
docker compose up --build
```

啟動後開啟：

| 服務 | URL |
|---|---|
| Frontend | http://localhost:3000 |
| Grafana | http://localhost:3001 |
| NATS Monitoring | http://localhost:8222 |
| Tempo API | http://localhost:3200 |

### 查看 Traces

1. 開啟 http://localhost:3001（Grafana，免登入）
2. 左側選 **Explore**
3. 資料來源選 **Tempo**（已自動配置）
4. 搜尋模式選 **Search**，Service Name 選 `frontend` 或 `api`
5. 點擊任一 trace 即可看到完整的 span 樹

## 專案結構

```
.
├── api/                        # API server（Go + Gin）
│   ├── Dockerfile
│   ├── go.mod                  # replace => ../pkg/nats.go
│   └── main.go                 # OTel + otelgin + NATS publish
├── worker/                     # WebSocket worker（Go）
│   ├── Dockerfile
│   ├── go.mod                  # replace => ../pkg/nats.go
│   └── main.go                 # NATS subscribe + WS broadcast
├── frontend/                   # React 前端（TypeScript）
│   ├── Dockerfile
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite-env.d.ts           # Vite 環境變數型別
│   ├── vite.config.js
│   ├── index.html
│   ├── nginx.conf
│   └── src/
│       ├── main.tsx
│       ├── App.tsx             # UI + OTel span 建立
│       └── tracing.ts          # OTel WebTracerProvider 初始化
├── pkg/
│   └── nats.go/                # Git submodule — NATS Go client fork
├── nats/
│   └── nats-server.conf
├── grafana/
│   └── provisioning/
│       └── datasources/
│           └── datasource.yml  # 自動配置 Tempo datasource
├── docker-compose.yml
├── otel-collector-config.yaml
├── tempo.yaml
└── LICENSE                     # Apache 2.0
```

## Git Submodule

本專案使用 [fork 的 NATS Go client](https://github.com/Marz32onE/nats.go) 作為 git submodule，位於 `pkg/nats.go`。`api` 和 `worker` 的 `go.mod` 中透過 `replace` 指令指向此本地路徑：

```
replace github.com/nats-io/nats.go => ../pkg/nats.go
```

修改 `pkg/nats.go` 中的程式碼後，`api` 和 `worker` 會立即使用到最新版本。

## 環境變數

### API

| 變數 | 預設值 | 說明 |
|---|---|---|
| `NATS_URL` | `nats://localhost:4222` | NATS 連線地址 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTel Collector gRPC 端點 |

### Worker

| 變數 | 預設值 | 說明 |
|---|---|---|
| `NATS_URL` | `nats://localhost:4222` | NATS 連線地址 |

### Frontend（Build-time）

| 變數 | 預設值 | 說明 |
|---|---|---|
| `VITE_API_URL` | `http://localhost:8081` | API server URL |
| `VITE_WS_URL` | `ws://localhost:8082` | WebSocket worker URL |
| `VITE_OTEL_COLLECTOR_URL` | `http://localhost:4318` | OTel Collector HTTP 端點 |

## License

[Apache License 2.0](LICENSE)

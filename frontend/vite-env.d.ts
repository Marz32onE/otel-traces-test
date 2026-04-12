/// <reference types="vite/client" />
/// <reference types="react" />
/// <reference types="react-dom" />

interface ImportMetaEnv {
  readonly VITE_API_URL: string
  readonly VITE_API_V1_URL: string
  readonly VITE_WS_URL: string
  readonly VITE_NATS_WS_URL: string
  readonly VITE_OTEL_COLLECTOR_URL: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

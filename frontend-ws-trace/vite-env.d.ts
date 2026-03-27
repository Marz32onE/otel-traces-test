/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_WS_OTEL_URL?: string;
  readonly VITE_WS_PLAIN_URL?: string;
  readonly VITE_OTEL_COLLECTOR_URL?: string;
}


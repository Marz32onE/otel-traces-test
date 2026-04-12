export const API_URL =
  import.meta.env.VITE_API_URL ?? "http://localhost:8088";
export const API_V1_URL =
  import.meta.env.VITE_API_V1_URL ?? "http://localhost:8089";
export const WS_URL = import.meta.env.VITE_WS_URL ?? "ws://localhost:8082";
/** NATS W3C WebSocket URL for browser `wsconnect` (see nats/nats-server.conf). */
export const NATS_WS_URL =
  import.meta.env.VITE_NATS_WS_URL ?? "ws://localhost:9222";

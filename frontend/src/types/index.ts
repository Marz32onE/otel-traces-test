export type LastTrace = {
  traceId: string;
  endpoint: string;
  id?: string;
} | null;

export type ApiResponse = {
  trace_id?: string;
  endpoint?: string;
  id?: string;
  ids?: string[];
  text?: string;
  error?: string;
};

export type SendPayload = {
  text?: string;
  id?: string;
  /** For bulk insert: array of message texts */
  texts?: string[];
  /** For bulk update: array of { id, text } */
  updates?: { id: string; text: string }[];
};

export type SendToEndpointOptions = {
  onSuccess?: () => void;
};

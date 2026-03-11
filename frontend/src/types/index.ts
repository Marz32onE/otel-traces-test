export type LastTrace = {
  traceId: string;
  endpoint: string;
  id?: string;
} | null;

export type ApiResponse = {
  trace_id?: string;
  endpoint?: string;
  id?: string;
  text?: string;
  error?: string;
};

export type SendPayload = {
  text?: string;
  id?: string;
};

export type SendToEndpointOptions = {
  onSuccess?: () => void;
};

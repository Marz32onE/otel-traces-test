{{/*
Shared OpenTelemetry / instrumentation env for api, worker, dbwatcher.
Usage: {{- include "otel-traces-test.otelEnv" .Values.api.env | nindent 12 }}
*/}}
{{- define "otel-traces-test.otelEnv" -}}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ .OTEL_EXPORTER_OTLP_ENDPOINT | quote }}
- name: OTEL_INSTRUMENTATION_GO_TRACING_ENABLED
  value: {{ .OTEL_INSTRUMENTATION_GO_TRACING_ENABLED | quote }}
- name: OTEL_MONGO_TRACING_ENABLED
  value: {{ .OTEL_MONGO_TRACING_ENABLED | quote }}
- name: OTEL_MONGO_PROPAGATION_ENABLED
  value: {{ .OTEL_MONGO_PROPAGATION_ENABLED | quote }}
- name: OTEL_NATS_TRACING_ENABLED
  value: {{ .OTEL_NATS_TRACING_ENABLED | quote }}
- name: OTEL_NATS_PROPAGATION_ENABLED
  value: {{ .OTEL_NATS_PROPAGATION_ENABLED | quote }}
- name: OTEL_GORILLA_WS_TRACING_ENABLED
  value: {{ .OTEL_GORILLA_WS_TRACING_ENABLED | quote }}
- name: OTEL_TRACES_SAMPLER
  value: {{ .OTEL_TRACES_SAMPLER | quote }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: {{ .OTEL_TRACES_SAMPLER_ARG | quote }}
{{- end }}

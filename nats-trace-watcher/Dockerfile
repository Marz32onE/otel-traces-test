FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY pkg/instrumentation-go ./pkg/instrumentation-go/
COPY pkg/otelsetup ./pkg/otelsetup/
COPY nats-trace-watcher/go.mod nats-trace-watcher/go.sum ./nats-trace-watcher/
RUN cd nats-trace-watcher && go mod download
COPY nats-trace-watcher/ ./nats-trace-watcher/
RUN cd nats-trace-watcher && go build -o nats-trace-watcher .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/nats-trace-watcher/nats-trace-watcher .
CMD ["./nats-trace-watcher"]

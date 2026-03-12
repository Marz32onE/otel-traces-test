FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY pkg/instrumentation-go ./pkg/instrumentation-go/
COPY worker/go.mod worker/go.sum ./worker/
RUN cd worker && go mod download
COPY worker/ ./worker/
RUN cd worker && go build -o worker .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/worker/worker .
EXPOSE 8082
CMD ["./worker"]

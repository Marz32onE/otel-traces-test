FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY worker/go.mod worker/go.sum ./worker/
COPY pkg/nats.go/ ./pkg/nats.go/
RUN cd worker && go mod download
COPY worker/ ./worker/
RUN cd worker && go build -o worker .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/worker/worker .
EXPOSE 8082
CMD ["./worker"]

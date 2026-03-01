FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY api/go.mod api/go.sum ./api/
COPY pkg/nats.go/ ./pkg/nats.go/
COPY pkg/nats.trace.go/ ./pkg/nats.trace.go/
RUN cd api && go mod download
COPY api/ ./api/
RUN cd api && go build -o api .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/api/api .
EXPOSE 8081
CMD ["./api"]

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY pkg/instrumentation-go ./pkg/instrumentation-go/
COPY api-mongo-v1/go.mod api-mongo-v1/go.sum ./api-mongo-v1/
RUN cd api-mongo-v1 && go mod download
COPY api-mongo-v1/ ./api-mongo-v1/
RUN cd api-mongo-v1 && go build -o api-mongo-v1 .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/api-mongo-v1/api-mongo-v1 .
EXPOSE 8089
CMD ["./api-mongo-v1"]

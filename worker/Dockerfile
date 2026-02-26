FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o worker .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/worker .
EXPOSE 8082
CMD ["./worker"]

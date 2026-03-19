FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY pkg/instrumentation-go ./pkg/instrumentation-go/
COPY pkg/otelsetup ./pkg/otelsetup/
COPY dbwatcher/go.mod dbwatcher/go.sum ./dbwatcher/
RUN cd dbwatcher && go mod download
COPY dbwatcher/ ./dbwatcher/
RUN cd dbwatcher && go build -o dbwatcher .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/dbwatcher/dbwatcher .
CMD ["./dbwatcher"]

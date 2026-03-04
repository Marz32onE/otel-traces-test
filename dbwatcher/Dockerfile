FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY pkg/natstrace ./pkg/natstrace/
COPY dbwatcher/go.mod ./dbwatcher/
RUN cd dbwatcher && go mod download
COPY dbwatcher/ ./dbwatcher/
RUN cd dbwatcher && go build -o dbwatcher .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/dbwatcher/dbwatcher .
CMD ["./dbwatcher"]

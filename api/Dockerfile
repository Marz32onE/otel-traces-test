FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY pkg/natstrace ./pkg/natstrace/
COPY pkg/mongodbtrace ./pkg/mongodbtrace/
COPY api/go.mod api/go.sum ./api/
RUN cd api && go mod download
COPY api/ ./api/
RUN cd api && go build -o api .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/api/api .
EXPOSE 8088
CMD ["./api"]

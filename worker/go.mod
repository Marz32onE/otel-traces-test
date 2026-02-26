module github.com/Marz32onE/otel-traces-test/worker

go 1.24.0

replace github.com/nats-io/nats.go => ../pkg/nats.go

require (
	github.com/gorilla/websocket v1.5.3
	github.com/nats-io/nats.go v1.37.0
)

require (
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/nats-io/nkeys v0.4.12 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

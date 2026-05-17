package otelsetup

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof on DefaultServeMux
	"os"
)

// ListenPprof starts the default HTTP mux on PPROF_ADDR (default ":6060") for pprof.
// Set PPROF_DISABLE=true to skip (e.g. production).
func ListenPprof() {
	if os.Getenv("PPROF_DISABLE") == "true" {
		return
	}
	addr := os.Getenv("PPROF_ADDR")
	if addr == "" {
		addr = ":6060"
	}
	go func() {
		log.Printf("pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("pprof server: %v", err)
		}
	}()
}

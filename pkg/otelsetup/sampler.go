package otelsetup

import (
	"os"
	"strconv"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SamplerFromEnv builds a root sampler from OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG.
// Uses TraceIDRatioBased (not ParentBased) so load tests with traceparent headers do not
// force 100% sampling when ratio is 0.01.
//
// Supported OTEL_TRACES_SAMPLER values:
//   - "" or "always_on" -> AlwaysSample
//   - "always_off" -> NeverSample
//   - "traceidratio" -> TraceIDRatioBased(ARG), default ARG 1.0
func SamplerFromEnv() sdktrace.Sampler {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	arg := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	switch name {
	case "", "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		ratio := 1.0
		if arg != "" {
			if f, err := strconv.ParseFloat(arg, 64); err == nil && f >= 0 && f <= 1 {
				ratio = f
			}
		}
		return sdktrace.TraceIDRatioBased(ratio)
	default:
		return sdktrace.AlwaysSample()
	}
}

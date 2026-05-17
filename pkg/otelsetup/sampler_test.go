package otelsetup

import "testing"

func TestSamplerFromEnv_alwaysOn(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	if SamplerFromEnv() == nil {
		t.Fatal("nil sampler")
	}
}

func TestSamplerFromEnv_traceidratio(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.01")
	if SamplerFromEnv() == nil {
		t.Fatal("nil sampler")
	}
}

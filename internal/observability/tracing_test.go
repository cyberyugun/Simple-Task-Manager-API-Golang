package observability

import (
	"context"
	"testing"
)

func TestParseTraceparent(t *testing.T) {
	traceID, spanID, flags, ok := parseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("expected valid traceparent")
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || spanID != "00f067aa0ba902b7" || flags != "01" {
		t.Fatalf("unexpected parsed traceparent: %s %s %s", traceID, spanID, flags)
	}
}

func TestParseTraceparentRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{
		"",
		"00-nothex-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	} {
		if _, _, _, ok := parseTraceparent(value); ok {
			t.Fatalf("parseTraceparent(%q) unexpectedly succeeded", value)
		}
	}
}

func TestDisabledTracer(t *testing.T) {
	tracer, err := NewTracer("", "task-api", "test", nil, NewMetrics())
	if err != nil {
		t.Fatalf("NewTracer() error = %v", err)
	}
	if tracer.Enabled() {
		t.Fatal("disabled tracer should not report enabled")
	}

	ctx, traceparent, finish := tracer.StartServerSpan(context.Background(), "GET", "/health", "", "")
	finish(200)
	if traceparent != "" {
		t.Fatalf("disabled tracer returned traceparent %q", traceparent)
	}
	if TraceIDFromContext(ctx) != "" {
		t.Fatal("disabled tracer should not add trace ID")
	}
}

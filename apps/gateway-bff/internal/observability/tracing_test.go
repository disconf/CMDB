package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func TestEnabledAndMiddlewareFollowTheEndpointSetting(t *testing.T) {
	withEnv(t, "OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if Enabled() {
		t.Fatal("tracing must stay disabled without an endpoint")
	}
	if Middleware() != nil {
		t.Fatal("middleware must be nil while tracing is disabled")
	}

	withEnv(t, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector.ops-qa.svc.cluster.local:4318")
	if !Enabled() {
		t.Fatal("tracing must be enabled once an endpoint is configured")
	}
	if Middleware() == nil {
		t.Fatal("middleware must be returned while tracing is enabled")
	}
}

func TestSetupTracingWithoutEndpointIsANoop(t *testing.T) {
	withEnv(t, "OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := SetupTracing(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown must not fail: %v", err)
	}
}

func TestMiddlewareRecordsServerSpan(t *testing.T) {
	withEnv(t, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector.ops-qa.svc.cluster.local:4318")
	withEnv(t, "OTEL_SERVICE_NAME", "gateway-bff-test")

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)

	middleware := Middleware()
	if middleware == nil {
		t.Fatal("middleware must be returned while tracing is enabled")
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/assets", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected exactly one span, got %d", len(spans))
	}
	if spans[0].Name != "GET /api/v1/cmdb/assets" {
		t.Fatalf("unexpected span name %q", spans[0].Name)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	if attributes["http.response.status_code"] != int64(http.StatusTeapot) {
		t.Fatalf("unexpected status attribute %v", attributes["http.response.status_code"])
	}
}

func TestParseHeaders(t *testing.T) {
	headers := parseHeaders(" authorization = Bearer abc , x-tenant=ops ,broken")
	if headers["authorization"] != "Bearer abc" || headers["x-tenant"] != "ops" {
		t.Fatalf("unexpected headers %v", headers)
	}
	if len(headers) != 2 {
		t.Fatalf("malformed entries must be skipped, got %v", headers)
	}
}

func TestSampleRatioFallsBackToFullSampling(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "2", "abc"} {
		withEnv(t, "OTEL_TRACES_SAMPLER_ARG", raw)
		if ratio := sampleRatio(); ratio != 1 {
			t.Fatalf("ratio for %q must fall back to 1, got %v", raw, ratio)
		}
	}
	withEnv(t, "OTEL_TRACES_SAMPLER_ARG", "0.25")
	if ratio := sampleRatio(); ratio != 0.25 {
		t.Fatalf("expected 0.25, got %v", ratio)
	}
}

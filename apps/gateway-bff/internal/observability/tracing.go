package observability

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const defaultServiceName = "gateway-bff"

// ServiceName resolves the OpenTelemetry service name.
func ServiceName() string {
	if value := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); value != "" {
		return value
	}
	return defaultServiceName
}

// Enabled reports whether an OTLP endpoint is configured.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != ""
}

// SetupTracing installs an OTLP/HTTP tracer provider. When no endpoint is
// configured it returns a no-op shutdown handler so the gateway keeps running
// unchanged in environments without a collector.
func SetupTracing(ctx context.Context) (func(context.Context) error, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	options := []otlptracehttp.Option{}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		options = append(options, otlptracehttp.WithEndpoint(parsed.Host))
		if parsed.Scheme != "https" {
			options = append(options, otlptracehttp.WithInsecure())
		}
		if path := strings.Trim(parsed.Path, "/"); path != "" {
			options = append(options, otlptracehttp.WithURLPath("/"+path+"/v1/traces"))
		}
	} else {
		options = append(options, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
	}
	if headers := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")); len(headers) > 0 {
		options = append(options, otlptracehttp.WithHeaders(headers))
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return func(context.Context) error { return nil }, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", ServiceName()))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio()))),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

// Middleware records one server span per request. It returns nil when tracing is
// disabled so callers can skip the wrapper entirely.
func Middleware() func(http.Handler) http.Handler {
	if !Enabled() {
		return nil
	}
	tracer := otel.Tracer(ServiceName())
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("url.path", r.URL.Path),
					attribute.String("http.route", r.URL.Path),
				))
			defer span.End()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r.WithContext(ctx))
			span.SetAttributes(attribute.Int("http.response.status_code", recorder.status))
			if recorder.status >= 500 {
				span.SetAttributes(attribute.Bool("error", true))
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack keeps WebSocket upgrades working behind tracing. Without it the
// recorder hides the underlying http.Hijacker and terminal connections fail
// with HTTP 500 after a successful ticket exchange.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func sampleRatio() float64 {
	raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	if raw == "" {
		return 1
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 1 {
		return 1
	}
	return value
}

func parseHeaders(raw string) map[string]string {
	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers
}

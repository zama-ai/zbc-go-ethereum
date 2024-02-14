package vm

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	otelsdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config represents a tracing configuration used upon initialization.
type OtlpConfig struct {
	ServiceName           string
	OtelCollectorEndpoint string
}

func init() {
	collectorEndpoint, present := os.LookupEnv("FHEVM_OTEL_COLLECTOR_ENDPOINT")
	if !present {
		collectorEndpoint = "localhost:4317"
	}
	cfg := &OtlpConfig{
		ServiceName:           "fhevm",
		OtelCollectorEndpoint: collectorEndpoint,
	}

	_, err := initTraceProvider(cfg)
	if err != nil {
		log.Fatal(err)
	}

	err = InitMeterProvider(cfg)
	if err != nil {
		log.Fatal(err)
	}
}

func initTraceProvider(cfg *OtlpConfig) (*otelsdk.TracerProvider, error) {
	var err error
	tp := &otelsdk.TracerProvider{}
	if cfg.OtelCollectorEndpoint != "" {
		tp, err = initOtelTracer(cfg)
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return tp, err
}

func initOtelTracer(cfg *OtlpConfig) (*otelsdk.TracerProvider, error) {
	conn, err := grpc.DialContext(context.Background(), cfg.OtelCollectorEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	tp := otelsdk.NewTracerProvider(
		otelsdk.WithSampler(otelsdk.AlwaysSample()),
		// Register the trace exporter with a TracerProvider, using a batch
		// span processor to aggregate spans before export.
		otelsdk.WithBatcher(traceExporter),
		otelsdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(cfg.ServiceName),
		)),
	)

	return tp, nil
}

func InitMeterProvider(cfg *OtlpConfig) error {
	res, err := newResource(cfg)
	if err != nil {
		return err
	}
	meterProvider, err := newMeterProvider(res)
	if err != nil {
		return err
	}
	otel.SetMeterProvider(meterProvider)
	return nil
}

func newResource(cfg *OtlpConfig) (*resource.Resource, error) {
	return resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		))
}

func newMeterProvider(res *resource.Resource) (*metric.MeterProvider, error) {
	metricExporter, err := stdoutmetric.New()
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
	)
	return meterProvider, nil
}

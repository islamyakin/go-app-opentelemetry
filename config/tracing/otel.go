package tracing

import (
	"context"
	"fmt"
	"github.com/islamyakin/go-app-opentelemtry/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"time"
)

func CreateGrpcConn() (*grpc.ClientConn, error) {

	conn, err := grpc.Dial(config.Otel_host,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GRPC connction: %v", err)
	}

	return conn, nil
}

func CreateResource(ctx context.Context) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceName(config.Name),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %v", err.Error())
	}

	return res, nil
}

func InitTraceProvider() (*trace.TracerProvider, error) {
	ctx := context.Background()

	grpcConn, err := CreateGrpcConn()
	if err != nil {
		log.Fatal(err)
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(grpcConn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// create resource
	res, err := CreateResource(ctx)
	if err != nil {
		return nil, err
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := trace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := trace.NewTracerProvider(
		// trace.WithSampler(trace.AlwaysSample()),
		trace.WithResource(res),
		trace.WithSpanProcessor(bsp),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(config.Sampler))), // head sampling
	)
	otel.SetTracerProvider(tracerProvider)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider, nil
}

func InitMeterProvider(ctx context.Context) (*metric.MeterProvider, error) {

	grpcConn, err := CreateGrpcConn()
	if err != nil {
		log.Fatal(err)
	}

	exp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithGRPCConn(grpcConn),
	)

	if err != nil {
		return nil, fmt.Errorf("can't init exporter: %v", err)
	}

	// create resource
	res, err := CreateResource(ctx)
	if err != nil {
		return nil, err
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(
			metric.NewPeriodicReader(exp, metric.WithInterval(3*time.Second)),
		),
		metric.WithResource(res),
	)

	otel.SetMeterProvider(mp)
	return mp, nil
}

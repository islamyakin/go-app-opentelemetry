package main

import (
	"context"
	"fmt"
	"log"
	"time"

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
)

func createGrpcConn() (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(otel_host,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to create GRPC  connection: %v", err)
	}
	return conn, nil
}

func createResource(ctx context.Context) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(name),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resources: %v", err.Error())
	}
	return res, nil
}

func initTraceProvider() (*trace.TracerProvider, error) {
	ctx := context.Background()

	grpcConn, err := createGrpcConn()
	if err != nil {
		log.Fatal(err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(grpcConn))
	if err != nil {
		return nil, fmt.Errorf("Failed to create trace exporter: %w", err)
	}

	res, err := createResource(ctx)
	if err != nil {
		return nil, err
	}

	bsp := trace.NewBatchSpanProcessor(traceExporter)
	traceProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(bsp),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(sampler))),
	)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return traceProvider, nil
}

func initMeterProvider(ctx context.Context) (*metric.MeterProvider, error) {
	grpcConn, err := createGrpcConn()
	if err != nil {
		log.Fatal(err)
	}

	exp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithGRPCConn(grpcConn),
	)
	if err != nil {
		return nil, fmt.Errorf("can't init exporter : %v", err)
	}

	res, err := createResource(ctx)
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

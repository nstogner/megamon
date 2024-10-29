package metrics

import (
	"context"
	"log"

	"example.com/megamon/internal/records"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
)

var (
	AggregationDuration metric.Float64Histogram
)

func initMeterProvider() *metricsdk.MeterProvider {
	// Create a Prometheus exporter
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatalf("failed to initialize prometheus exporter: %v", err)
	}

	// Create a MeterProvider and register it globally
	provider := metricsdk.NewMeterProvider(metricsdk.WithReader(exporter))
	otel.SetMeterProvider(provider)

	return provider
}

type Reporter interface {
	Report() records.Report
}

func Init(r Reporter) func() {
	// Initialize the OpenTelemetry Prometheus exporter and meter provider
	provider := initMeterProvider()

	meter := otel.Meter("megamon")

	var err error
	AggregationDuration, err = meter.Float64Histogram("megamon.aggregation.duration",
		metric.WithDescription("Duration of the aggregation loop."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60),
	)
	if err != nil {
		log.Fatalf("failed to create aggregation.duration histogram: %v", err)
	}

	jobsetUp, err := meter.Int64ObservableGauge("megamon.jobset.up",
		metric.WithDescription("Whether all JobSet Job replicas are ready."),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.up gauge: %v", err)
	}

	jobsetNodesUp, err := meter.Int64ObservableGauge("megamon.jobset.nodes.up",
		metric.WithDescription("Whether all Nodes for a JobSet are ready."),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.nodes.up gauge: %v", err)
	}

	jobsetUpTime, err := meter.Float64ObservableCounter("megamon.jobset.uptime",
		metric.WithDescription("Total time JobSet has been up."),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.uptime counter: %v", err)
	}
	jobsetInterruptionTime, err := meter.Float64ObservableCounter("megamon.jobset.intteruptiontime",
		metric.WithDescription("Total time JobSet has interrupted."),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.uptime counter: %v", err)
	}

	jobsetInterruptions, err := meter.Int64ObservableCounter("megamon.jobset.interruptions",
		metric.WithDescription("Total number of interruptions for a JobSet."),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.interruptions counter: %v", err)
	}

	jobsetRecoveries, err := meter.Int64ObservableCounter("megamon.jobset.recoveries",
		metric.WithDescription("Total number of recoveries for a JobSet."),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.recoveries counter: %v", err)
	}

	jobsetMTTR, err := meter.Float64ObservableGauge("megamon.jobset.mttr",
		metric.WithDescription("Mean Time To Recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.mttr gauge: %v", err)
	}

	jobsetMTBI, err := meter.Float64ObservableGauge("megamon.jobset.mtbi",
		metric.WithDescription("Mean Time Between Interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.mtbi gauge: %v", err)
	}

	jobsetTTIUp, err := meter.Float64ObservableGauge("megamon.jobset.ttiup",
		metric.WithDescription("Time-To-Initial-Up state for a JobSet."),
		metric.WithUnit("s"),
	)
	if err != nil {
		log.Fatalf("failed to create jobset.ttiup gauge: %v", err)
	}

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		report := r.Report()

		for jobsetName, jobsetReport := range report.JobSetsUp {
			val := int64(0)
			if jobsetReport.Up {
				val = 1
			}
			o.ObserveInt64(jobsetUp, val, metric.WithAttributes(
				jobsetOTELAttrs(jobsetName, jobsetReport.JobSetAttrs)...,
			))
		}

		for jobsetName, jobsetNodeReport := range report.JobSetNodesUp {
			val := int64(0)
			if jobsetNodeReport.Up() {
				val = 1
			}
			o.ObserveInt64(jobsetNodesUp, val, metric.WithAttributes(
				jobsetOTELAttrs(jobsetName, jobsetNodeReport.JobSetAttrs)...,
			))
		}

		for jobsetName, jobsetSummary := range report.JobSetsUpSummaries {
			commonAttrs := jobsetOTELAttrs(jobsetName, jobsetSummary.JobSetAttrs)
			o.ObserveInt64(jobsetInterruptions, int64(jobsetSummary.Interruptions), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(jobsetRecoveries, int64(jobsetSummary.Recoveries), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetUpTime, jobsetSummary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetInterruptionTime, jobsetSummary.InterruptionTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetMTTR, jobsetSummary.MTTR.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetMTBI, jobsetSummary.MTBI.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetTTIUp, jobsetSummary.TTIUp.Seconds(), metric.WithAttributes(commonAttrs...))
		}

		return nil
	},
		jobsetUp,
		jobsetNodesUp,
		jobsetInterruptions,
		jobsetRecoveries,
		jobsetUpTime,
		jobsetInterruptionTime,
		jobsetMTTR,
		jobsetMTBI,
		jobsetTTIUp,
	)
	if err != nil {
		log.Fatalf("failed to register callback: %v", err)
	}

	// Return a function that can be used to shutdown the provider.
	return func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			log.Printf("failed to shutdown MeterProvider: %v", err)
		}
	}
}

func jobsetOTELAttrs(name string, attrs records.JobSetAttrs) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("jobset.name", name),
		attribute.String("tpu.topology", attrs.TPUTopology),
		attribute.String("tpu.accelerator", attrs.TPUAccelerator),
		attribute.Bool("spot", attrs.Spot),
	}
}

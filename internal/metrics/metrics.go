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
	fatal(err)

	// Jobset //

	jobsetUp, err := meter.Int64ObservableGauge("megamon.jobset.up",
		metric.WithDescription("Whether all JobSet Job replicas are ready."),
	)
	fatal(err)

	jobsetUpTime, err := meter.Float64ObservableCounter("megamon.jobset.uptime",
		metric.WithDescription("Total time JobSet has been up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetInterruptionTime, err := meter.Float64ObservableCounter("megamon.jobset.intteruptiontime",
		metric.WithDescription("Total time JobSet has interrupted."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetInterruptions, err := meter.Int64ObservableCounter("megamon.jobset.interruptions",
		metric.WithDescription("Total number of interruptions for a JobSet."),
	)
	fatal(err)

	jobsetRecoveries, err := meter.Int64ObservableCounter("megamon.jobset.recoveries",
		metric.WithDescription("Total number of recoveries for a JobSet."),
	)
	fatal(err)

	jobsetMTTR, err := meter.Float64ObservableGauge("megamon.jobset.mttr",
		metric.WithDescription("Mean Time To Recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetTTR, err := meter.Float64ObservableGauge("megamon.jobset.ttr",
		metric.WithDescription("Last Time To Recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetMTBI, err := meter.Float64ObservableGauge("megamon.jobset.mtbi",
		metric.WithDescription("Mean Time Between Interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetTBI, err := meter.Float64ObservableGauge("megamon.jobset.tbi",
		metric.WithDescription("Last Time Between Interruption for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetTTIUp, err := meter.Float64ObservableGauge("megamon.jobset.ttiup",
		metric.WithDescription("Time-To-Initial-Up state for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	// Jobset Nodes //

	jobsetNodesUp, err := meter.Int64ObservableGauge("megamon.jobset.nodes.up",
		metric.WithDescription("Whether all Nodes for a JobSet are ready."),
	)
	fatal(err)

	jobsetNodeUpTime, err := meter.Float64ObservableCounter("megamon.jobset.nodes.uptime",
		metric.WithDescription("Total time a JobSets Nodes have been up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeInterruptionTime, err := meter.Float64ObservableCounter("megamon.jobset.nodes.intteruptiontime",
		metric.WithDescription("Total time a JobSets Nodes have been interrupted."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeInterruptions, err := meter.Int64ObservableCounter("megamon.jobset.nodes.interruptions",
		metric.WithDescription("Total number of interruptions for a JobSets Nodes."),
	)
	fatal(err)

	jobsetNodeRecoveries, err := meter.Int64ObservableCounter("megamon.jobset.nodes.recoveries",
		metric.WithDescription("Total number of recoveries for a JobSets Nodes."),
	)
	fatal(err)

	jobsetNodeMTTR, err := meter.Float64ObservableGauge("megamon.jobset.nodes.mttr",
		metric.WithDescription("Mean Time To Recovery for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeTTR, err := meter.Float64ObservableGauge("megamon.jobset.nodes.ttr",
		metric.WithDescription("Last Time To Recovery for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeMTBI, err := meter.Float64ObservableGauge("megamon.jobset.nodes.mtbi",
		metric.WithDescription("Mean Time Between Interruptions for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeTBI, err := meter.Float64ObservableGauge("megamon.jobset.nodes.tbi",
		metric.WithDescription("Last Time Between Interruptions for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodeTTIUp, err := meter.Float64ObservableGauge("megamon.jobset.nodes.ttiup",
		metric.WithDescription("Time-To-Initial-Up state for a JobSet Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		report := r.Report()

		for _, jobsetReport := range report.JobSetsUp {
			val := int64(0)
			if jobsetReport.Up() {
				val = 1
			}
			o.ObserveInt64(jobsetUp, val, metric.WithAttributes(
				OTELAttrs(jobsetReport.Attrs)...,
			))
		}

		for _, jobsetNodeReport := range report.JobSetNodesUp {
			val := int64(0)
			if jobsetNodeReport.Up() {
				val = 1
			}
			o.ObserveInt64(jobsetNodesUp, val, metric.WithAttributes(
				OTELAttrs(jobsetNodeReport.Attrs)...,
			))
		}

		for _, summary := range report.JobSetsUpSummaries {
			commonAttrs := OTELAttrs(summary.Attrs)
			o.ObserveInt64(jobsetInterruptions, int64(summary.Interruptions), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(jobsetRecoveries, int64(summary.Recoveries), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetUpTime, summary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetInterruptionTime, summary.InterruptionTime.Seconds(), metric.WithAttributes(commonAttrs...))
			if summary.MTTR != 0 {
				o.ObserveFloat64(jobsetMTTR, summary.MTTR.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TTR != 0 {
				o.ObserveFloat64(jobsetTTR, summary.TTR.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MTBI != 0 {
				o.ObserveFloat64(jobsetMTBI, summary.MTBI.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TBI != 0 {
				o.ObserveFloat64(jobsetTBI, summary.TBI.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TTIUp != 0 {
				o.ObserveFloat64(jobsetTTIUp, summary.TTIUp.Seconds(), metric.WithAttributes(commonAttrs...))
			}
		}
		for _, summary := range report.JobSetNodesUpSummaries {
			commonAttrs := OTELAttrs(summary.Attrs)
			o.ObserveInt64(jobsetNodeInterruptions, int64(summary.Interruptions), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(jobsetNodeRecoveries, int64(summary.Recoveries), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetNodeUpTime, summary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetNodeInterruptionTime, summary.InterruptionTime.Seconds(), metric.WithAttributes(commonAttrs...))
			if summary.MTTR != 0 {
				o.ObserveFloat64(jobsetNodeMTTR, summary.MTTR.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TTR != 0 {
				o.ObserveFloat64(jobsetNodeTTR, summary.TTR.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MTBI != 0 {
				o.ObserveFloat64(jobsetNodeMTBI, summary.MTBI.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TBI != 0 {
				o.ObserveFloat64(jobsetNodeTBI, summary.TBI.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TTIUp != 0 {
				o.ObserveFloat64(jobsetNodeTTIUp, summary.TTIUp.Seconds(), metric.WithAttributes(commonAttrs...))
			}
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
		jobsetTTR,
		jobsetMTBI,
		jobsetTBI,
		jobsetTTIUp,
		jobsetNodeInterruptions,
		jobsetNodeRecoveries,
		jobsetNodeUpTime,
		jobsetNodeInterruptionTime,
		jobsetNodeMTTR,
		jobsetNodeTTR,
		jobsetNodeMTBI,
		jobsetNodeTBI,
		jobsetNodeTTIUp,
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

func OTELAttrs(attrs records.Attrs) []attribute.KeyValue {
	var otelAttrs []attribute.KeyValue
	if attrs.JobSetNamespace != "" {
		otelAttrs = append(otelAttrs, attribute.String("jobset.namespace", attrs.JobSetNamespace))
	}
	if attrs.JobSetName != "" {
		otelAttrs = append(otelAttrs, attribute.String("jobset.name", attrs.JobSetName))
	}
	if attrs.TPUTopology != "" {
		otelAttrs = append(otelAttrs, attribute.String("tpu.topology", attrs.TPUTopology))
	}
	if attrs.TPUAccelerator != "" {
		otelAttrs = append(otelAttrs, attribute.String("tpu.accelerator", attrs.TPUAccelerator))
	}
	if attrs.Spot {
		otelAttrs = append(otelAttrs, attribute.Bool("spot", attrs.Spot))
	}
	return otelAttrs
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}

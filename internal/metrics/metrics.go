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
	Prefix              = "megamon"
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
	AggregationDuration, err = meter.Float64Histogram(Prefix+".aggregation.duration",
		metric.WithDescription("Duration of the aggregation loop."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60),
	)
	fatal(err)

	// Jobset //

	jobsetUp, err := meter.Int64ObservableGauge(Prefix+".jobset.up",
		metric.WithDescription("Whether all JobSet Job replicas are in a Ready status (0 or 1)."),
	)
	fatal(err)

	jobsetUpTime, err := meter.Float64ObservableCounter(Prefix+".jobset.up.time",
		metric.WithDescription("Total time JobSet has been up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	// Note: Technically this could be a counter if we are fully certain that the value
	// will never decrease. In practice, this caused issues where this value was being
	// reported inaccurately. It is possible that this was because the timeseries was
	// not fully unique across JobSet instances at that time.
	jobsetDownTime, err := meter.Float64ObservableGauge(Prefix+".jobset.down.time",
		metric.WithDescription("Total time JobSet has not been fully up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetInterruptionCount, err := meter.Int64ObservableCounter(Prefix+".jobset.interruption.count",
		metric.WithDescription("Total number of interruptions for a JobSet."),
	)
	fatal(err)

	jobsetRecoveryCount, err := meter.Int64ObservableCounter(Prefix+".jobset.recovery.count",
		metric.WithDescription("Total number of recoveries for a JobSet."),
	)
	fatal(err)

	jobsetDownTimeInitial, err := meter.Float64ObservableGauge(Prefix+".jobset.down.time.initial",
		metric.WithDescription("Initial time elapsed before JobSet is first comes up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetDownTimeBetweenRecovery, err := meter.Float64ObservableGauge(Prefix+".jobset.down.time.between.recovery",
		metric.WithDescription("Total time spent down between being all interruptions and recoveries."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetDownTimeBetweenRecoveryMean, err := meter.Float64ObservableGauge(Prefix+".jobset.down.time.between.recovery.mean",
		metric.WithDescription("Mean time to recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetDownTimeBetweenRecoveryLatest, err := meter.Float64ObservableGauge(Prefix+".jobset.down.time.between.recovery.latest",
		metric.WithDescription("Last time to recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetUpTimeBetweenInterruption, err := meter.Float64ObservableGauge(Prefix+".jobset.up.time.between.interruption",
		metric.WithDescription("Total time between interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetUpTimeBetweenInterruptionMean, err := meter.Float64ObservableGauge(Prefix+".jobset.up.time.between.interruption.mean",
		metric.WithDescription("Mean time between interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetUpTimeBetweenInterruptionLatest, err := meter.Float64ObservableGauge(Prefix+".jobset.up.time.between.interruption.latest",
		metric.WithDescription("Last time between interruption for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	// Jobset Nodes //

	jobsetNodesUp, err := meter.Int64ObservableGauge(Prefix+".jobset.nodes.up",
		metric.WithDescription("Whether all Nodes for a JobSet are in a Ready status (0 or 1)."),
	)
	fatal(err)

	jobsetNodesUpTime, err := meter.Float64ObservableCounter(Prefix+".jobset.nodes.up.time",
		metric.WithDescription("Total time a JobSets Nodes have been up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesDownTime, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.down.time",
		metric.WithDescription("Total time a JobSets Nodes have not all been Ready (up)."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesInterruptionCount, err := meter.Int64ObservableCounter(Prefix+".jobset.nodes.interruption.count",
		metric.WithDescription("Total number of interruptions for a JobSets Nodes."),
	)
	fatal(err)

	jobsetNodesRecoveryCount, err := meter.Int64ObservableCounter(Prefix+".jobset.nodes.recovery.count",
		metric.WithDescription("Total number of recoveries for a JobSets Nodes."),
	)
	fatal(err)

	jobsetNodesDownTimeBetweenRecovery, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.down.time.between.recovery",
		metric.WithDescription("Total time spent recovering for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesDownTimeBetweenRecoveryMean, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.down.time.between.recovery.mean",
		metric.WithDescription("Mean time to recovery for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesDownTimeBetweenRecoveryLatest, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.down.time.between.recovery.latest",
		metric.WithDescription("Last time To recovery for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesUpTimeBetweenInterruption, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.up.time.between.interruption",
		metric.WithDescription("Total time between interruptions for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesUpTimeBetweenInterruptionMean, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.up.time.between.interruption.mean",
		metric.WithDescription("Mean time between interruptions for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesUpTimeBetweenInterruptionLatest, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.up.time.between.interruption.latest",
		metric.WithDescription("Last time between interruptions for a JobSets Nodes."),
		metric.WithUnit("s"),
	)
	fatal(err)

	jobsetNodesDownTimeInitial, err := meter.Float64ObservableGauge(Prefix+".jobset.nodes.down.time.initial",
		metric.WithDescription("Time elapsed before all JobSet Nodes are Ready (up) for the first time."),
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
			o.ObserveInt64(jobsetInterruptionCount, int64(summary.InterruptionCount), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(jobsetRecoveryCount, int64(summary.RecoveryCount), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetUpTime, summary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetDownTime, summary.DownTime.Seconds(), metric.WithAttributes(commonAttrs...))
			if summary.DownTimeInitial != 0 {
				o.ObserveFloat64(jobsetDownTimeInitial, summary.DownTimeInitial.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetDownTimeBetweenRecovery, summary.TotalDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetDownTimeBetweenRecoveryMean, summary.MeanDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetDownTimeBetweenRecoveryLatest, summary.LatestDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetUpTimeBetweenInterruption, summary.TotalUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetUpTimeBetweenInterruptionMean, summary.MeanUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetUpTimeBetweenInterruptionLatest, summary.LatestUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
		}
		for _, summary := range report.JobSetNodesUpSummaries {
			commonAttrs := OTELAttrs(summary.Attrs)
			o.ObserveInt64(jobsetNodesInterruptionCount, int64(summary.InterruptionCount), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(jobsetNodesRecoveryCount, int64(summary.RecoveryCount), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetNodesUpTime, summary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(jobsetNodesDownTime, summary.DownTime.Seconds(), metric.WithAttributes(commonAttrs...))
			if summary.DownTimeInitial != 0 {
				o.ObserveFloat64(jobsetNodesDownTimeInitial, summary.DownTimeInitial.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetNodesDownTimeBetweenRecovery, summary.TotalDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetNodesDownTimeBetweenRecoveryMean, summary.MeanDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(jobsetNodesDownTimeBetweenRecoveryLatest, summary.LatestDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetNodesUpTimeBetweenInterruption, summary.TotalUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetNodesUpTimeBetweenInterruptionMean, summary.MeanUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(jobsetNodesUpTimeBetweenInterruptionLatest, summary.LatestUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
		}

		return nil
	},
		jobsetUp,
		jobsetUpTime,
		jobsetUpTimeBetweenInterruption,
		jobsetUpTimeBetweenInterruptionMean,
		jobsetUpTimeBetweenInterruptionLatest,
		jobsetDownTime,
		jobsetDownTimeInitial,
		jobsetDownTimeBetweenRecovery,
		jobsetDownTimeBetweenRecoveryMean,
		jobsetDownTimeBetweenRecoveryLatest,
		jobsetInterruptionCount,
		jobsetRecoveryCount,
		jobsetNodesUp,
		jobsetNodesUpTime,
		jobsetNodesUpTimeBetweenInterruption,
		jobsetNodesUpTimeBetweenInterruptionMean,
		jobsetNodesUpTimeBetweenInterruptionLatest,
		jobsetNodesDownTime,
		jobsetNodesDownTimeInitial,
		jobsetNodesDownTimeBetweenRecovery,
		jobsetNodesDownTimeBetweenRecoveryMean,
		jobsetNodesDownTimeBetweenRecoveryLatest,
		jobsetNodesInterruptionCount,
		jobsetNodesRecoveryCount,
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

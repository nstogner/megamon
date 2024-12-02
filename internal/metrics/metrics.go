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
	// Initialize the OpenTelemetry Prometheus exporter and meter provider.
	provider := initMeterProvider()

	meter := otel.Meter("megamon")

	var err error
	AggregationDuration, err = meter.Float64Histogram(Prefix+".aggregation.duration",
		metric.WithDescription("Duration of the aggregation loop."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60),
	)
	fatal(err)

	nodePoolJobScheduled, err := meter.Int64ObservableGauge(Prefix+".nodepool.job.scheduled",
		metric.WithDescription("Whether a JobSet's Job is scheduled on a NodePool (0 or 1)."),
	)
	fatal(err)

	jobsetObservables, observeJobset := mustRegisterUpnessMetrics(Prefix+".jobset", meter)
	jobsetNodeObservables, observeJobsetNodes := mustRegisterUpnessMetrics(Prefix+".jobset.nodes", meter)
	nodePoolObservables, observeNodePools := mustRegisterUpnessMetrics(Prefix+".nodepool", meter)

	observables := append(jobsetObservables, jobsetNodeObservables...)
	observables = append(observables, nodePoolObservables...)
	observables = append(observables, nodePoolJobScheduled)

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		report := r.Report()

		observeJobset(ctx, o, report.JobSetsUp, report.JobSetsUpSummaries)
		observeJobsetNodes(ctx, o, report.JobSetNodesUp, report.JobSetNodesUpSummaries)
		observeNodePools(ctx, o, report.NodePoolsUp, report.NodePoolsUpSummaries)

		for npName, sch := range report.NodePoolScheduling {
			o.ObserveInt64(nodePoolJobScheduled, 1, metric.WithAttributes(
				attribute.String("nodepool.name", npName),
				attribute.String("job.name", sch.JobName),
				attribute.String("jobset.name", sch.JobSetName),
			))
		}

		return nil
	},
		observables...,
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
	if attrs.JobSetUID != "" {
		otelAttrs = append(otelAttrs, attribute.String("jobset.uid", attrs.JobSetUID))
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
	if attrs.NodePoolName != "" {
		otelAttrs = append(otelAttrs, attribute.String("nodepool.name", attrs.NodePoolName))
	}
	return otelAttrs
}

type reportObserveFunc func(ctx context.Context, o metric.Observer, ups map[string]records.Upness, summaries map[string]records.UpnessSummaryWithAttrs)

// mustRegisterUpnessMetrics registers a set of metrics for observing the upness of something.
func mustRegisterUpnessMetrics(prefix string, meter metric.Meter) ([]metric.Observable, reportObserveFunc) {
	up, err := meter.Int64ObservableGauge(prefix+".up",
		metric.WithDescription("Whether all JobSet Job replicas are in a Ready status (0 or 1)."),
	)
	fatal(err)

	upTime, err := meter.Float64ObservableCounter(prefix+".up.time",
		metric.WithDescription("Total time JobSet has been up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	// Note: Technically this could be a counter if we are fully certain that the value
	// will never decrease. In practice, this caused issues where this value was being
	// reported inaccurately. It is possible that this was because the timeseries was
	// not fully unique across JobSet instances at that time.
	downTime, err := meter.Float64ObservableGauge(prefix+".down.time",
		metric.WithDescription("Total time JobSet has not been fully up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	interruptionCount, err := meter.Int64ObservableCounter(prefix+".interruption.count",
		metric.WithDescription("Total number of interruptions for a JobSet."),
	)
	fatal(err)

	recoveryCount, err := meter.Int64ObservableCounter(prefix+".recovery.count",
		metric.WithDescription("Total number of recoveries for a JobSet."),
	)
	fatal(err)

	downTimeInitial, err := meter.Float64ObservableGauge(prefix+".down.time.initial",
		metric.WithDescription("Initial time elapsed before JobSet is first comes up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecovery, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery",
		metric.WithDescription("Total time spent down between being all interruptions and recoveries."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecoveryMean, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery.mean",
		metric.WithDescription("Mean time to recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecoveryLatest, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery.latest",
		metric.WithDescription("Last time to recovery for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruption, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption",
		metric.WithDescription("Total time between interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruptionMean, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption.mean",
		metric.WithDescription("Mean time between interruptions for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruptionLatest, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption.latest",
		metric.WithDescription("Last time between interruption for a JobSet."),
		metric.WithUnit("s"),
	)
	fatal(err)

	observeFunc := func(ctx context.Context, o metric.Observer, upnesses map[string]records.Upness, summaries map[string]records.UpnessSummaryWithAttrs) {
		for _, upness := range upnesses {
			val := int64(0)
			if upness.Up() {
				val = 1
			}
			o.ObserveInt64(up, val, metric.WithAttributes(
				OTELAttrs(upness.Attrs)...,
			))
		}

		for _, summary := range summaries {
			commonAttrs := OTELAttrs(summary.Attrs)
			o.ObserveInt64(interruptionCount, int64(summary.InterruptionCount), metric.WithAttributes(commonAttrs...))
			o.ObserveInt64(recoveryCount, int64(summary.RecoveryCount), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(upTime, summary.UpTime.Seconds(), metric.WithAttributes(commonAttrs...))
			o.ObserveFloat64(downTime, summary.DownTime.Seconds(), metric.WithAttributes(commonAttrs...))
			if summary.DownTimeInitial != 0 {
				o.ObserveFloat64(downTimeInitial, summary.DownTimeInitial.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(downTimeBetweenRecovery, summary.TotalDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(downTimeBetweenRecoveryMean, summary.MeanDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestDownTimeBetweenRecovery != 0 {
				o.ObserveFloat64(downTimeBetweenRecoveryLatest, summary.LatestDownTimeBetweenRecovery.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.TotalUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(upTimeBetweenInterruption, summary.TotalUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.MeanUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(upTimeBetweenInterruptionMean, summary.MeanUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
			if summary.LatestUpTimeBetweenInterruption != 0 {
				o.ObserveFloat64(upTimeBetweenInterruptionLatest, summary.LatestUpTimeBetweenInterruption.Seconds(), metric.WithAttributes(commonAttrs...))
			}
		}
	}

	return []metric.Observable{
		up,
		upTime,
		upTimeBetweenInterruption,
		upTimeBetweenInterruptionMean,
		upTimeBetweenInterruptionLatest,
		downTime,
		downTimeInitial,
		downTimeBetweenRecovery,
		downTimeBetweenRecoveryMean,
		downTimeBetweenRecoveryLatest,
		interruptionCount,
		recoveryCount,
	}, observeFunc
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}

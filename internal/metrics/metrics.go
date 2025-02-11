package metrics

import (
	"context"
	"os"
	"time"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	AggregationDuration metric.Float64Histogram
	Prefix              = "megamon"
	log                 = logf.Log.WithName("metrics")
)

func initMeterProvider(ctx context.Context, interval time.Duration) *metricsdk.MeterProvider {
	// Create a Prometheus exporter
	promExporter, err := prometheus.New()
	if err != nil {
		log.Error(err, "failed to initialize Prometheus exporter")
		os.Exit(1)
	}
	grpcExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		log.Error(err, "failed to initialize OTLP gRPC exporter")
		os.Exit(1)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("megamon"),
		),
	)
	if err != nil {
		log.Error(err, "Error creating resource")
		os.Exit(1)
	}

	// Create a MeterProvider and register it globally
	provider := metricsdk.NewMeterProvider(
		metricsdk.WithResource(res),
		metricsdk.WithReader(promExporter),
		metricsdk.WithReader(metricsdk.NewPeriodicReader(grpcExporter,
			metricsdk.WithInterval(interval),
		)),
	)
	otel.SetMeterProvider(provider)

	return provider
}

type Reporter interface {
	ReportReady() bool
	Report() records.Report
}

func Init(ctx context.Context, r Reporter, interval time.Duration) func() {
	// Initialize the OpenTelemetry Prometheus exporter and meter provider.
	provider := initMeterProvider(ctx, interval)

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

	tpuChipCount, err := meter.Int64ObservableGauge(Prefix+".jobset.tpu.chip.count",
		metric.WithDescription("Total number of TPU chips."))
	fatal(err)

	jobsetObservables, observeJobset := mustRegisterUpnessMetrics(Prefix+".jobset", meter)
	jobsetNodeObservables, observeJobsetNodes := mustRegisterUpnessMetrics(Prefix+".jobset.nodes", meter)
	nodePoolObservables, observeNodePools := mustRegisterUpnessMetrics(Prefix+".nodepool", meter)

	observables := append(jobsetObservables, jobsetNodeObservables...)
	observables = append(observables, nodePoolObservables...)
	observables = append(observables, nodePoolJobScheduled)
	observables = append(observables, tpuChipCount)

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		if !r.ReportReady() {
			return nil
		}

		report := r.Report()

		observeJobset(ctx, o, report.JobSetsUp, report.JobSetsUpSummaries)
		observeJobsetNodes(ctx, o, report.JobSetNodesUp, report.JobSetNodesUpSummaries)
		observeNodePools(ctx, o, report.NodePoolsUp, report.NodePoolsUpSummaries)
		observeTpuChipCount(o, tpuChipCount, report.NodePoolScheduling, report.JobSetNodesUp, report.NodePoolsUp)

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
		log.Error(err, "failed to register callback")
		os.Exit(1)
	}

	// Return a function that can be used to shutdown the provider.
	return func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			log.Error(err, "failed to shutdown MeterProvider")
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
		metric.WithDescription("Whether all replicas are in a Ready status (0 or 1)."),
	)
	fatal(err)

	// NOTE: Gauges are used instead of Counters because Megamon restarts
	// can show up as different timeseries if they are scraped using a scraper that
	// adds a label for the megamon Pod name (when scraped via `kind: PodMonitoring` in
	// Google Managed Prometheus on GKE).

	upTime, err := meter.Float64ObservableGauge(prefix+".up.time",
		metric.WithDescription("Total time up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTime, err := meter.Float64ObservableGauge(prefix+".down.time",
		metric.WithDescription("Total time down."),
		metric.WithUnit("s"),
	)
	fatal(err)

	interruptionCount, err := meter.Int64ObservableGauge(prefix+".interruption.count",
		metric.WithDescription("Total number of interruptions."),
	)
	fatal(err)

	recoveryCount, err := meter.Int64ObservableGauge(prefix+".recovery.count",
		metric.WithDescription("Total number of recoveries."),
	)
	fatal(err)

	downTimeInitial, err := meter.Float64ObservableGauge(prefix+".down.time.initial",
		metric.WithDescription("Initial time elapsed before first up."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecovery, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery",
		metric.WithDescription("Total time spent down between being all interruptions and recoveries."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecoveryMean, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery.mean",
		metric.WithDescription("Mean time to recovery."),
		metric.WithUnit("s"),
	)
	fatal(err)

	downTimeBetweenRecoveryLatest, err := meter.Float64ObservableGauge(prefix+".down.time.between.recovery.latest",
		metric.WithDescription("Last time to recovery."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruption, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption",
		metric.WithDescription("Total time between interruptions."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruptionMean, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption.mean",
		metric.WithDescription("Mean time between interruptions."),
		metric.WithUnit("s"),
	)
	fatal(err)

	upTimeBetweenInterruptionLatest, err := meter.Float64ObservableGauge(prefix+".up.time.between.interruption.latest",
		metric.WithDescription("Last time between interruption."),
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

func observeTpuChipCount(o metric.Observer, tpuChipCountMetric metric.Int64ObservableGauge, schJobs map[string]records.ScheduledJob, jobSetNodesUp map[string]records.Upness, nodepools map[string]records.Upness) {
	log.V(3).Info("observeTpuChipCount")
	// create map of {jobsetName} -> [leaderjobNames, ...]
	jobsByJobset := map[string]map[string]string{}
	for npName, sch := range schJobs {
		if jobsByJobset[sch.JobSetName] == nil {
			jobsByJobset[sch.JobSetName] = make(map[string]string)
		}
		jobsByJobset[sch.JobSetName][sch.JobName] = npName
	}
	log.V(3).Info("jobsByJobset map", "jobsByJobset", jobsByJobset)
	log.V(3).Info("jobSetNodesUp map", "jobSetNodesUp", jobSetNodesUp)
	log.V(3).Info("nodePoolScheduling map", "schJobs", schJobs)

	// for each job uid, lookup leaderjobs by jobset
	for uid, js := range jobSetNodesUp {
		var metricAttrs []attribute.KeyValue
		jobsetName := js.Attrs.JobSetName
		jobsetNamespace := js.Attrs.JobSetNamespace
		metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "jobset.uid", Value: attribute.StringValue(uid)})
		metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "jobset.name", Value: attribute.StringValue(jobsetName)})
		for leaderJob, npName := range jobsByJobset[jobsetName] {
			tpuTopology := nodepools[npName].TPUTopology
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "tpu.topology", Value: attribute.StringValue(tpuTopology)})
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "tpu.accelerator", Value: attribute.StringValue(js.Attrs.TPUAccelerator)})
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "nodepool.name", Value: attribute.StringValue(npName)})
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "spot", Value: attribute.BoolValue(js.Attrs.Spot)})
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "job.name", Value: attribute.StringValue(leaderJob)})
			metricAttrs = append(metricAttrs, attribute.KeyValue{Key: "job.namespace", Value: attribute.StringValue(jobsetNamespace)})
			if chipCount, err := k8sutils.TpuTopologyToChipCount(tpuTopology); err != nil {
				log.Error(err, "error geting chip count for job", "namespace", jobsetNamespace, "jobsetName", jobsetName, "job", leaderJob, "topology", tpuTopology)
			} else {
				o.ObserveInt64(tpuChipCountMetric, int64(chipCount), metric.WithAttributes(metricAttrs...))
			}
		}
	}
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}

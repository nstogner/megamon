package manager

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	containerv1beta1 "google.golang.org/api/container/v1beta1"

	"cloud.google.com/go/storage"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"example.com/megamon/internal/aggregator"
	"example.com/megamon/internal/controller"
	"example.com/megamon/internal/gcsclient"
	"example.com/megamon/internal/gkeclient"
	"example.com/megamon/internal/metrics"
	"example.com/megamon/internal/records"

	// +kubebuilder:scaffold:imports

	slice "example.com/megamon/copied-slice-api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	//utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(jobset.AddToScheme(scheme.Scheme))
	utilruntime.Must(slice.AddToScheme(scheme.Scheme))
}

type Config struct {
	MetricsPrefix string

	AggregationIntervalSeconds int64
	Exporters                  []string

	ReportConfigMapRef types.NamespacedName

	EventsBucketName string
	EventsBucketPath string

	DisableNodePoolJobLabelling bool

	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	SecureMetrics        bool
	EnableHTTP2          bool
	EnableSimulation     bool

	// GKE client options
	GKE GKEConfig

	// Controller runtime thresholds
	UnknownCountThreshold float64

	// HyperComputer features
	SliceEnabled bool
}

type GKEConfig struct {
	ProjectID       string
	ClusterLocation string
	ClusterName     string
}

func (c GKEConfig) ClusterRef() string {
	return fmt.Sprintf("projects/%s/locations/%s/clusters/%s",
		c.ProjectID,
		c.ClusterLocation,
		c.ClusterName,
	)
}

func MustConfigure() Config {
	var configPath string
	flag.StringVar(&configPath, "config-path", "/etc/megamon/config.json", "The location of the config file.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfgFile, err := os.ReadFile(configPath)
	if err != nil {
		setupLog.Error(err, "unable to read config file")
		os.Exit(1)
	}
	// Define config defaults.
	cfg := Config{
		MetricsPrefix:              "megamon",
		AggregationIntervalSeconds: 10,
		ReportConfigMapRef: types.NamespacedName{
			Namespace: "megamon-system",
			Name:      "megamon-report",
		},
		DisableNodePoolJobLabelling: true,
		MetricsAddr:                 ":8080",
		EnableLeaderElection:        false,
		ProbeAddr:                   ":8081",
		SecureMetrics:               true,
		EnableHTTP2:                 false,
		UnknownCountThreshold:       1.0,
		EnableSimulation:            false,
	}

	if err := json.Unmarshal(cfgFile, &cfg); err != nil {
		setupLog.Error(err, "unable to unmarshal config file")
		os.Exit(1)
	}

	if cfg.UnknownCountThreshold < 0 || cfg.UnknownCountThreshold > 1 {
		setupLog.Error(nil, "unknown count threshold must be between 0 and 1", "threshold", cfg.UnknownCountThreshold)
		os.Exit(1)
	}

	if err := configureGKE(context.Background(), &cfg.GKE); err != nil {
		setupLog.Error(err, "unable to configure gke client")
		os.Exit(1)
	}

	if cfg.EventsBucketPath == "" {
		cfg.EventsBucketPath = fmt.Sprintf("megamon/clusters/%s", cfg.GKE.ClusterName)
	}

	if cfg.EnableSimulation {
		setupLog.Info("*** SIMULATION MODE Enabled ***")
	}

	return cfg
}

func configureGKE(ctx context.Context, cfg *GKEConfig) error {
	// Attempt to infer cluster information from GKE metadata server.
	md := metadata.NewClient(&http.Client{Timeout: 10 * time.Second})

	var err error
	if cfg.ProjectID == "" {
		cfg.ProjectID, err = md.ProjectIDWithContext(ctx)
		if err != nil {
			return fmt.Errorf("fetching project-id from metadata server because it was not set in config file: %w", err)
		}
	}
	if cfg.ClusterName == "" {
		cfg.ClusterName, err = md.InstanceAttributeValueWithContext(ctx, "cluster-name")
		if err != nil {
			return fmt.Errorf("fetching cluster-name from metadata server because it was not set in config file: %w", err)
		}
	}
	if cfg.ClusterLocation == "" {
		cfg.ClusterLocation, err = md.InstanceAttributeValueWithContext(ctx, "cluster-location")
		if err != nil {
			return fmt.Errorf("fetching cluster-location from metadata server because it was not set in config file: %w", err)
		}
	}

	return nil
}

type GKEClient interface {
	ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error)
}

type GCSClient interface {
	GetRecords(ctx context.Context, bucket, path string) (map[string]records.EventRecords, error)
	PutRecords(ctx context.Context, bucket, path string, recs map[string]records.EventRecords) error
}

func MustRun(ctx context.Context, cfg Config, restConfig *rest.Config, gkeClient GKEClient, gcsClient GCSClient) {
	setupLog.Info("starting manager with config", "config", cfg)
	metrics.Prefix = cfg.MetricsPrefix

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	var tlsOpts []func(*tls.Config)
	if !cfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		// Disable in favor of the otel metrics server.
		BindAddress:   "0", // metricsAddr,
		SecureServing: cfg.SecureMetrics,
		// TODO(user): TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts: tlsOpts,
	}

	if cfg.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// Only watch JobSet leader pods so that Jobs can be bound to the node
	// pools that they are scheduled on.
	//
	// jobset.sigs.k8s.io/jobset-name (exists)
	// batch.kubernetes.io/job-completion-index: "0"
	//
	jobsetPodSelector, err := labels.Parse("jobset.sigs.k8s.io/jobset-name, batch.kubernetes.io/job-completion-index=0")
	if err != nil {
		setupLog.Error(err, "unable to create jobset pod selector")
		os.Exit(1)
	}
	// Only watch Pods that are already scheduled to a Node.
	scheduledPodSelector, err := fields.ParseSelector("spec.nodeName!=")
	if err != nil {
		setupLog.Error(err, "unable to create scheduled pod selector")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.ProbeAddr,
		LeaderElection:         cfg.EnableLeaderElection,
		LeaderElectionID:       "fd0479f1.example.com",
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Pod{}: {
					Label: jobsetPodSelector,
					Field: scheduledPodSelector,
				},
			},
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if cfg.EnableSimulation {
		gkeClient = gkeclient.CreateStubGKEClient()
		gcsClient = gcsclient.CreateStubGCSClient()
	}

	if gkeClient == nil {
		containersService, err := containerv1beta1.NewService(context.Background())
		if err != nil {
			setupLog.Error(err, "unable to create gke client")
			os.Exit(1)
		}
		gkeClient = &gkeclient.Client{
			ClusterRef:        cfg.GKE.ClusterRef(),
			ContainersService: containersService,
		}
	}

	if gcsClient == nil {
		storageClient, err := storage.NewClient(ctx)
		if err != nil {
			setupLog.Error(err, "unable to create gcs client")
			os.Exit(1)
		}
		gcsClient = &gcsclient.Client{
			StorageClient: storageClient,
		}
	}

	agg := &aggregator.Aggregator{
		Interval:              time.Duration(cfg.AggregationIntervalSeconds) * time.Second,
		Client:                mgr.GetClient(),
		Exporters:             map[string]aggregator.Exporter{},
		GKE:                   gkeClient,
		GCS:                   gcsClient,
		EventsBucketName:      cfg.EventsBucketName,
		EventsBucketPath:      cfg.EventsBucketPath,
		UnknownCountThreshold: cfg.UnknownCountThreshold,
		SliceEnabled:          cfg.SliceEnabled,
	}

	availableExporters := map[string]aggregator.Exporter{
		"configmap": &aggregator.ConfigMapExporter{
			Client: mgr.GetClient(),
			Ref:    cfg.ReportConfigMapRef,
			Key:    "report",
		},
		"stdout": &aggregator.StdoutExporter{},
	}
	for _, name := range cfg.Exporters {
		if exporter, ok := availableExporters[name]; ok {
			agg.Exporters[name] = exporter
		} else {
			setupLog.Error(errors.New("exporter not found"), "exporter not found", "exporter", name)
			os.Exit(1)
		}
	}

	shutdownMetricsFunc := metrics.Init(ctx, agg, time.Duration(cfg.AggregationIntervalSeconds)*time.Second, cfg.UnknownCountThreshold)

	if err = (&controller.JobSetReconciler{
		Disabled: false,
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "JobSet")
		os.Exit(1)
	}
	if err = (&controller.NodeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}
	if err = (&controller.PodReconciler{
		Client:                      mgr.GetClient(),
		Scheme:                      mgr.GetScheme(),
		Aggregator:                  agg,
		DisableNodePoolJobLabelling: cfg.DisableNodePoolJobLabelling,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	//mgr.Add(agg)

	// Initial aggregation to populate the initial metrics report.
	// TODO: Verify the readiness check is applied before scraping.
	//if err := agg.Aggregate(ctx); err != nil {
	//	setupLog.Error(err, "failed initial aggregate")
	//}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		if !agg.ReportReady() {
			return errors.New("aggregator report not ready")
		}
		return nil
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	defer shutdownMetricsFunc()
	metricsMux := http.NewServeMux()
	promHandler := promhttp.Handler()
	metricsMux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !agg.ReportReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		promHandler.ServeHTTP(w, r)
	}))
	metricsServer := http.Server{Handler: metricsMux, Addr: cfg.MetricsAddr}

	var wg sync.WaitGroup

	mgr.Add(agg)
	//wg.Add(1)
	//go func() {
	//	log.Println("starting aggregator")
	//	defer wg.Done()
	//	if err := agg.Start(ctx); err != nil {
	//		if errors.Is(err, context.Canceled) {
	//			setupLog.Info("aggregator - context cancelled")
	//		} else {
	//			setupLog.Error(err, "aggregator error")
	//			os.Exit(1)
	//		}
	//	}
	//}()

	wg.Add(1)
	go func() {
		setupLog.Info("starting metrics server")
		defer wg.Done()
		if err := metricsServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				setupLog.Info("metrics server closed")
			} else if !errors.Is(err, context.Canceled) {
				setupLog.Info("metrics server - context cancelled")
			} else {
				setupLog.Error(err, "metrics server error")
				os.Exit(1)
			}
		}
	}()
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
	}
	metricsServer.Shutdown(context.Background())

	setupLog.Info("waiting for all goroutines to stop")
	wg.Wait()
	setupLog.Info("all goroutines stopped, exiting")
}

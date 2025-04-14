package aggregator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"example.com/megamon/internal/experiments"
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/metrics"
	"example.com/megamon/internal/records"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

type Aggregator struct {
	client.Client

	EventsBucketName string
	EventsBucketPath string

	Interval time.Duration

	reportMtx   sync.RWMutex
	report      records.Report
	reportReady bool

	nodePoolSchedulingMtx sync.RWMutex
	// map[<nodepool-name>]<details-about-what-is-scheduled-on-it>
	nodePoolScheduling map[string]records.ScheduledJob

	Exporters map[string]Exporter

	GKE GKEClient
	GCS GCSClient
}

var log = logf.Log.WithName("aggregator")

type GKEClient interface {
	ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error)
}

type GCSClient interface {
	GetRecords(ctx context.Context, bucket, path string) (map[string]records.EventRecords, error)
	PutRecords(ctx context.Context, bucket, path string, recs map[string]records.EventRecords) error
}

type Exporter interface {
	Export(context.Context, records.Report) error
}

func (a *Aggregator) ReportReady() bool {
	a.reportMtx.RLock()
	defer a.reportMtx.RUnlock()
	return a.reportReady
}

func (a *Aggregator) Start(ctx context.Context) error {
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			log.Info("aggregating")
		}

		start := time.Now()
		if err := a.Aggregate(ctx); err != nil {
			log.Error(err, "failed to aggregate")
			continue
		}
		metrics.AggregationDuration.Record(ctx, time.Since(start).Seconds())

		for name, exporter := range a.Exporters {
			if err := exporter.Export(ctx, a.Report()); err != nil {
				log.Error(err, "failed to export", "exporter", name)
			}
		}
	}
}

func (a *Aggregator) Report() records.Report {
	a.reportMtx.RLock()
	defer a.reportMtx.RUnlock()
	return a.report
}

func (a *Aggregator) Aggregate(ctx context.Context) error {
	report := records.NewReport()

	var jobsetList jobset.JobSetList
	if err := a.List(ctx, &jobsetList); err != nil {
		return fmt.Errorf("listing jobsets: %w", err)
	}

	now := time.Now()

	uidMapKey := func(ns, name string) string {
		return fmt.Sprintf("%s/%s", ns, name)
	}
	// map[<ns>/<name>]<uid>
	uidMap := map[string]string{}

	for _, js := range jobsetList.Items {
		if js.Status.TerminalState != "" {
			log.Info("jobset terminal state", "jobset", js.Name, "state", js.Status.TerminalState)
		}
		if !k8sutils.IsJobSetActive(&js) {
			log.V(1).Info("Skipping inactive jobset", "jobset", js.Name)
			continue
		}

		uid := string(js.UID)
		uidMap[uidMapKey(js.Namespace, js.Name)] = uid

		attrs := extractJobSetAttrs(&js)
		specReplicas, readyReplicas := k8sutils.GetJobSetReplicas(&js)
		report.JobSetsUp[uid] = records.Upness{
			ExpectedCount: specReplicas,
			ReadyCount:    readyReplicas,
			Attrs:         attrs,
		}
		report.JobSetNodesUp[uid] = records.Upness{
			ExpectedCount: k8sutils.GetExpectedNodeCount(&js),
			Attrs:         attrs,
		}
	}

	var nodeList corev1.NodeList
	if err := a.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}

	npList, err := a.GKE.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("listing node pools: %w", err)
	}
	for _, np := range npList {
		func() {
			if !isTPUNodePool(np) {
				return
			}
			up := records.Upness{
				Attrs: extractNodePoolAttrs(np),
			}
			expectedCount, err := getExpectedTPUNodePoolSize(np)
			if err != nil {
				log.Error(err, "failed to get expected TPU node pool size", "nodepool", np.Name)
				return
			}
			up.ExpectedCount = expectedCount
			if tpuChipCount, err := k8sutils.GetTpuTopologyToChipCount(up.TPUTopology); err != nil {
				log.Error(err, "failed to convert TPU topology to chip count", "nodepool", np.Name)
			} else {
				up.TPUChipCount = int32(tpuChipCount)
			}
			report.NodePoolsUp[np.Name] = up
		}()
	}

	for _, node := range nodeList.Items {
		nodeStatus := k8sutils.IsNodeReady(&node)

		// Node pool mapping:

		if npName, ok := k8sutils.GetNodePool(&node); ok {
			func() {
				if !k8sutils.IsTPUNode(&node) {
					log.V(5).Info("skipping non-tpu node", "nodepool", npName, "node", node.Name)
					return
				}
				up, ok := report.NodePoolsUp[npName]
				if !ok {
					log.Info("found Node for node pool that was not parsed", "node", node.Name, "nodepool", npName)
					return
				}
				if up.ExpectedCount == 0 { // this should be unexpected?
					log.Info("up.ExpectedCount == 0 for node pool", "nodepool", npName)
					var err error
					up.ExpectedCount, err = k8sutils.GetExpectedTPUNodePoolSize(&node)
					if err != nil {
						log.Error(err, "failed to get expected TPU node pool size", "node", node.Name)
						return
					}
				}
				if nodeStatus == k8sutils.NodeStatusReady {
					up.ReadyCount++
				} else if experiments.IsExperimentEnabled("NodeUnknownAsNotReady") {
					if nodeStatus == k8sutils.NodeStatusUnknown {
						up.UnknownCount++
					}
				}
				report.NodePoolsUp[npName] = up
			}()
		}

		// Static jobset mapping:
		if jsNS, jsName := k8sutils.GetJobSetForNode(&node); jsNS != "" && jsName != "" {
			func() {
				if jsNS == "" || jsName == "" {
					return
				}
				uid, ok := uidMap[uidMapKey(jsNS, jsName)]
				if !ok {
					return
				}

				up, ok := report.JobSetNodesUp[uid]
				if !ok {
					return
				}
				if nodeStatus == k8sutils.NodeStatusReady {
					up.ReadyCount++
				} else if experiments.IsExperimentEnabled("NodeUnknownAsNotReady") {
					if nodeStatus == k8sutils.NodeStatusUnknown {
						up.UnknownCount++
					}
				}
				report.JobSetNodesUp[uid] = up
			}()
		}
	}

	log.V(3).Info("DEBUG", "report.NodePoolsUp", report.NodePoolsUp, "report.JobSetNodesUp", report.JobSetNodesUp, "report.JobSetsUp", report.JobSetsUp)

	log.V(1).Info("reconciling jobset events")
	ctx = context.WithValue(ctx, records.LogKey{}, "jobset")
	jsEvents, err := a.reconcileEvents(ctx, "jobsets.json", report.JobSetsUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset events: %w", err)
	}
	log.V(1).Info("reconciling jobset node events")
	ctx = context.WithValue(ctx, records.LogKey{}, "jobset-nodes")
	jsNodeEvents, err := a.reconcileEvents(ctx, "jobset-nodes.json", report.JobSetNodesUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset node events: %w", err)
	}
	log.V(1).Info("reconciling nodepool events")
	ctx = context.WithValue(ctx, records.LogKey{}, "nodepool")
	nodePoolEvents, err := a.reconcileEvents(ctx, "node-pools.json", report.NodePoolsUp)
	if err != nil {
		return fmt.Errorf("reconciling nodepool events: %w", err)
	}

	log.V(1).Info("summarize jobsets")
	for key, events := range jsEvents {
		ctx = context.WithValue(ctx, records.LogKey{}, records.LogKeyValue{Type: "jobset", Key: key})
		log.V(1).Info("summarizing jobset events", "jobset", key)
		eventSummary := events.Summarize(ctx, now)
		report.JobSetsUpSummaries[key] = records.UpnessSummaryWithAttrs{
			Attrs:        report.JobSetsUp[key].Attrs,
			EventSummary: eventSummary,
		}
	}

	log.V(1).Info("summarize JobSetNodes events")
	for key, events := range jsNodeEvents {
		ctx = context.WithValue(ctx, records.LogKey{}, records.LogKeyValue{Type: "jobset-nodes", Key: key})
		log.V(1).Info("summarizing node events", "JobSetNodes", key)
		eventSummary := events.Summarize(ctx, now)
		report.JobSetNodesUpSummaries[key] = records.UpnessSummaryWithAttrs{
			Attrs:        report.JobSetNodesUp[key].Attrs,
			EventSummary: eventSummary,
		}
	}

	log.V(1).Info("summarize nodepool events")
	for key, events := range nodePoolEvents {
		ctx = context.WithValue(ctx, records.LogKey{}, records.LogKeyValue{Type: "nodepool", Key: key})
		log.V(1).Info("summarizing nodepool events", "nodepool", key)
		eventSummary := events.Summarize(ctx, now)
		report.NodePoolsUpSummaries[key] = records.UpnessSummaryWithAttrs{
			Attrs:        report.NodePoolsUp[key].Attrs,
			EventSummary: eventSummary,
		}
	}

	a.pruneNodePoolScheduling(report.NodePoolsUp)
	report.NodePoolScheduling = a.getNodePoolScheduling()

	a.reportMtx.Lock()
	a.report = report
	a.reportReady = true
	a.reportMtx.Unlock()

	return nil
}

func (a *Aggregator) reconcileEvents(ctx context.Context, filename string, ups map[string]records.Upness) (map[string]records.EventRecords, error) {
	path := strings.TrimSuffix(a.EventsBucketPath, "/") + "/" + filename
	recs, err := a.GCS.GetRecords(ctx, a.EventsBucketName, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get %q: %w", filename, err)
	}

	if changed := records.ReconcileEvents(ctx, time.Now(), ups, recs); changed {
		if err := a.GCS.PutRecords(ctx, a.EventsBucketName, path, recs); err != nil {
			return nil, fmt.Errorf("failed to put %q: %w", filename, err)
		}
	}

	return recs, nil
}

func (a *Aggregator) SetNodePoolScheduling(nodePoolName string, job records.ScheduledJob) {
	a.nodePoolSchedulingMtx.Lock()
	defer a.nodePoolSchedulingMtx.Unlock()
	if a.nodePoolScheduling == nil {
		a.nodePoolScheduling = make(map[string]records.ScheduledJob)
	}
	a.nodePoolScheduling[nodePoolName] = job
}

func (a *Aggregator) pruneNodePoolScheduling(nps map[string]records.Upness) {
	a.nodePoolSchedulingMtx.Lock()
	defer a.nodePoolSchedulingMtx.Unlock()
	for npName := range a.nodePoolScheduling {
		if _, ok := nps[npName]; !ok {
			delete(a.nodePoolScheduling, npName)
		}
	}
}

func (a *Aggregator) getNodePoolScheduling() map[string]records.ScheduledJob {
	a.nodePoolSchedulingMtx.RLock()
	defer a.nodePoolSchedulingMtx.RUnlock()
	cp := make(map[string]records.ScheduledJob, len(a.nodePoolScheduling))
	for k, v := range a.nodePoolScheduling {
		cp[k] = v
	}
	return cp
}

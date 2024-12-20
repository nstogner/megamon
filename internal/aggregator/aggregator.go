package aggregator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/metrics"
	"example.com/megamon/internal/records"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

type Aggregator struct {
	client.Client

	JobSetEventsConfigMapRef     types.NamespacedName
	JobSetNodeEventsConfigMapRef types.NamespacedName
	NodePoolEventsConfigMapRef   types.NamespacedName

	Interval time.Duration

	reportMtx   sync.RWMutex
	report      records.Report
	reportReady bool

	nodePoolSchedulingMtx sync.RWMutex
	// map[<nodepool-name>]<details-about-what-is-scheduled-on-it>
	nodePoolScheduling map[string]records.ScheduledJob

	Exporters map[string]Exporter

	GKE GKEClient
}

type GKEClient interface {
	ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error)
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
			log.Println("aggregating")
		}

		start := time.Now()
		if err := a.Aggregate(ctx); err != nil {
			log.Printf("failed to aggregate: %v", err)
			continue
		}
		metrics.AggregationDuration.Record(ctx, time.Since(start).Seconds())

		for name, exporter := range a.Exporters {
			if err := exporter.Export(ctx, a.Report()); err != nil {
				log.Printf("failed to export %s: %v", name, err)
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
		if !k8sutils.IsJobSetActive(&js) {
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
				log.Printf("ERROR: failed to get expected TPU node pool size for node pool %q: %v", np.Name, err)
				return
			}
			up.ExpectedCount = expectedCount
			report.NodePoolsUp[np.Name] = up
		}()
	}

	for _, node := range nodeList.Items {
		ready := k8sutils.IsNodeReady(&node)

		// Node pool mapping:

		if npName, ok := k8sutils.GetNodePool(&node); ok {
			func() {
				if !k8sutils.IsTPUNode(&node) {
					return
				}
				up, ok := report.NodePoolsUp[npName]
				if !ok {
					log.Printf("WARNING: found Node (%q) for node pool (%q) that was not parsed", node.Name, npName)
					return
				}
				if up.ExpectedCount == 0 {
					var err error
					up.ExpectedCount, err = k8sutils.GetExpectedTPUNodePoolSize(&node)
					if err != nil {
						log.Printf("failed to get expected TPU node pool size for node %q: %v", node.Name, err)
						return
					}
				}
				if ready {
					up.ReadyCount++
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
				if ready {
					up.ReadyCount++
				}
				report.JobSetNodesUp[uid] = up
			}()
		}
	}

	jsEvents, err := reconcileEvents(ctx, a.Client, a.JobSetEventsConfigMapRef, report.JobSetsUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset events: %w", err)
	}
	jsNodeEvents, err := reconcileEvents(ctx, a.Client, a.JobSetNodeEventsConfigMapRef, report.JobSetNodesUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset node events: %w", err)
	}
	nodePoolEvents, err := reconcileEvents(ctx, a.Client, a.NodePoolEventsConfigMapRef, report.NodePoolsUp)
	if err != nil {
		return fmt.Errorf("reconciling nodepool events: %w", err)
	}

	for key, events := range jsEvents {
		eventSummary := events.Summarize(now)
		report.JobSetsUpSummaries[key] = records.UpnessSummaryWithAttrs{
			Attrs:        report.JobSetsUp[key].Attrs,
			EventSummary: eventSummary,
		}
	}
	for key, events := range jsNodeEvents {
		eventSummary := events.Summarize(now)
		report.JobSetNodesUpSummaries[key] = records.UpnessSummaryWithAttrs{
			Attrs:        report.JobSetNodesUp[key].Attrs,
			EventSummary: eventSummary,
		}
	}
	for key, events := range nodePoolEvents {
		eventSummary := events.Summarize(now)
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

func reconcileEvents(ctx context.Context, client client.Client, cmRef types.NamespacedName, ups map[string]records.Upness) (map[string]records.EventRecords, error) {
	var cm corev1.ConfigMap
	if err := client.Get(ctx, cmRef, &cm); err != nil {
		return nil, fmt.Errorf("failed to get event records configmap: %w", err)
	}

	recs, err := k8sutils.GetEventRecordsFromConfigMap(&cm)
	if err != nil {
		return nil, fmt.Errorf("failed to get event records from configmap: %w", err)
	}

	if changed := records.ReconcileEvents(time.Now(), ups, recs); changed {
		if err := k8sutils.SetEventRecordsInConfigMap(&cm, recs); err != nil {
			return nil, fmt.Errorf("failed to set event records in configmap: %w", err)
		}

		if err := client.Update(ctx, &cm); err != nil {
			return nil, fmt.Errorf("failed to update events configmap: %w", err)
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

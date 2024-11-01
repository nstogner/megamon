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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

type Aggregator struct {
	client.Client

	JobSetEventsConfigMapRef     types.NamespacedName
	JobSetNodeEventsConfigMapRef types.NamespacedName

	Interval time.Duration

	reportMtx   sync.RWMutex
	report      records.Report
	reportReady bool

	Exporters map[string]Exporter
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

	//	expectedCMEventKeys := make(map[string]struct{})

	now := time.Now()

	for _, js := range jobsetList.Items {
		if !k8sutils.IsJobSetActive(&js) {
			continue
		}

		//expectedCMEventKeys[k8sutils.JobSetEventsKey(&js)] = struct{}{}
		key := jobsetKey(js.Namespace, js.Name)

		attrs := extractJobSetAttrs(&js)
		specReplicas, readyReplicas := k8sutils.GetJobSetReplicas(&js)
		report.JobSetsUp[key] = records.Upness{
			ExpectedCount: specReplicas,
			ReadyCount:    readyReplicas,
			Attrs:         attrs,
		}
		report.JobSetNodesUp[key] = records.Upness{
			ExpectedCount: k8sutils.GetExpectedNodeCount(&js),
			Attrs:         attrs,
		}
	}

	var nodeList corev1.NodeList
	if err := a.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		jsNS, jsName := k8sutils.GetJobSetForNode(&node)
		if jsNS == "" || jsName == "" {
			continue
		}
		key := jobsetKey(jsNS, jsName)
		up, ok := report.JobSetNodesUp[key]
		if !ok {
			continue
		}
		if !k8sutils.IsNodeReady(&node) {
			continue
		}
		up.ReadyCount++
		report.JobSetNodesUp[key] = up
	}

	jsEvents, err := reconcileEvents(ctx, a.Client, a.JobSetEventsConfigMapRef, report.JobSetsUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset events: %w", err)
	}
	jsNodeEvents, err := reconcileEvents(ctx, a.Client, a.JobSetNodeEventsConfigMapRef, report.JobSetNodesUp)
	if err != nil {
		return fmt.Errorf("reconciling jobset events: %w", err)
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

	changed, err := records.ReconcileEvents( /*r.mode,*/ ups, recs)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile events: %w", err)
	}

	if changed {
		if err := k8sutils.SetEventRecordsInConfigMap(&cm, recs); err != nil {
			return nil, fmt.Errorf("failed to set event records in configmap: %w", err)
		}

		if err := client.Update(ctx, &cm); err != nil {
			return nil, fmt.Errorf("failed to update events configmap: %w", err)
		}
	}

	return recs, nil
}

func jobsetKey(namespace, name string) string {
	return namespace + "." + name
}

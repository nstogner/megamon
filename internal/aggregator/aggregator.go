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
	"sigs.k8s.io/controller-runtime/pkg/client"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

type Aggregator struct {
	client.Client

	Interval time.Duration

	reportMtx   sync.RWMutex
	report      records.Report
	reportReady bool

	Exporters map[string]Exporter
}

type Exporter interface {
	Export(records.Report) error
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

		t0 := time.Now()
		if err := a.Aggregate(ctx); err != nil {
			log.Printf("failed to aggregate: %v", err)
			continue
		}
		metrics.AggregationDuration.Record(ctx, time.Since(t0).Seconds())

		for name, exporter := range a.Exporters {
			if err := exporter.Export(a.Report()); err != nil {
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

	for _, js := range jobsetList.Items {
		if !k8sutils.IsJobSetActive(&js) {
			continue
		}
		attrs := extractJobSetAttrs(&js)
		report.JobSetsUp[js.Name] = records.JobSetUp{
			Up:          k8sutils.IsJobSetUp(&js),
			JobSetAttrs: attrs,
		}
		report.JobSetNodesUp[js.Name] = records.JobSetNodesUp{
			ExpectedCount: k8sutils.GetExpectedNodeCount(&js),
			JobSetAttrs:   attrs,
		}

		metaRecords, err := k8sutils.GetJobsetRecords(&js)
		if err == nil {
			report.JobSetsUpSummaries[js.Name] = records.EventSummaryWithAttrs{
				JobSetAttrs:  attrs,
				EventSummary: metaRecords.Summarize(time.Now()),
			}
		} else {
			log.Printf("failed to get jobset records: %v", err)
		}
	}

	var nodeList corev1.NodeList
	if err := a.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		jobsetName, ok := k8sutils.GetJobSetForNode(&node)
		if !ok {
			continue
		}
		up, ok := report.JobSetNodesUp[jobsetName]
		if !ok {
			continue
		}
		if !k8sutils.IsNodeReady(&node) {
			continue
		}
		up.ReadyCount++

		report.JobSetNodesUp[jobsetName] = up
	}

	a.reportMtx.Lock()
	a.report = report
	a.reportReady = true
	a.reportMtx.Unlock()

	return nil
}

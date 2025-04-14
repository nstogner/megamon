package records

import (
	"context"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("events")

const JobSetRecordsAnnotationKey = "megamon.tbd/records"

type LogKey struct{}
type LogKeyValue struct {
	Type string
	Key  string
}

type EventRecords struct {
	UpEvents []UpEvent `json:"upEvents"`
}

type UpEvent struct {
	Up        bool      `json:"up"`
	Timestamp time.Time `json:"ts"`
}

type UpnessSummaryWithAttrs struct {
	Attrs
	EventSummary
}

type EventSummary struct {
	// DownTimeInitial is the time spent before the system was up.
	DownTimeInitial time.Duration `json:"downTimeProvisioned"`

	// InterruptionCount is the number of times that the system has gone down after being up.
	InterruptionCount int `json:"interruptionCount"`
	// RecoveryCount is the number of times that the system has recovered from a down state.
	RecoveryCount int `json:"recoveryCount"`

	// DownTime is the total time spent in the down state.
	DownTime time.Duration `json:"downTime"`
	// UpTime is the total time spent in an up state.
	UpTime time.Duration `json:"upTime"`

	TotalDownTimeBetweenRecovery time.Duration `json:"totalDownTimeBetweenRecovery"`
	// TotalUpTimeBetweenInterruption - Total Time Between Interruption
	TotalUpTimeBetweenInterruption time.Duration `json:"totalUpTimeBetweenInterruption"`

	// LatestDownTimeBetweenRecovery - Last Time To Recovery
	LatestDownTimeBetweenRecovery time.Duration `json:"latestDownTimeBetweenRecovery"`
	// LatestUpTimeBetweenInterruption - Last Time Between Interruption
	LatestUpTimeBetweenInterruption time.Duration `json:"latestUpTimeBetweenInterruption"`

	// MeanDownTimeBetweenRecovery - Mean Time To Recovery
	MeanDownTimeBetweenRecovery time.Duration `json:"meanDownTimeBetweenRecovery"`
	// MeanUpTimeBetweenInterruption - Mean Time Between Interruption
	MeanUpTimeBetweenInterruption time.Duration `json:"meanUpTimeBetweenInterruption"`
}

func (r *EventRecords) Summarize(ctx context.Context, now time.Time) EventSummary {
	var summary EventSummary

	summaryLog := log.WithValues("key", ctx.Value(LogKey{}))
	n := len(r.UpEvents)
	summaryLog.V(3).Info("summarizing events", "event_count", n)
	if n == 0 {
		return summary
	}
	if r.UpEvents[0].Up {
		// Invalid data.
		summaryLog.V(3).Info("invalid data: first event is up")
		return summary
	}
	if n == 1 {
		summary.DownTime = now.Sub(r.UpEvents[0].Timestamp)
		return summary
	}
	// Invalid or missing data:
	if !r.UpEvents[1].Up {
		summaryLog.V(3).Info("invalid data: second event is not up")
		return summary
	}

	summary.DownTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	summary.DownTimeInitial = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)

	// up:        ___
	// down:  ____|
	// event: 0   1

	if len(r.UpEvents) == 2 {
		summary.UpTime = now.Sub(r.UpEvents[1].Timestamp)
		return summary
	}

	// up:        _____
	// down:  ____|   |
	// event: 0   1   2
	for i := 2; i < len(r.UpEvents); i++ {
		if r.UpEvents[i].Up {
			// Just transitioned down to up.
			summary.LatestDownTimeBetweenRecovery = r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.DownTime += summary.LatestDownTimeBetweenRecovery
			summary.TotalDownTimeBetweenRecovery += summary.LatestDownTimeBetweenRecovery
			summary.RecoveryCount++
			summaryLog.V(5).Info("recovery event found, incrementing count")
		} else {
			// Just transitioned up to down.
			summary.LatestUpTimeBetweenInterruption = r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.UpTime += summary.LatestUpTimeBetweenInterruption
			summary.TotalUpTimeBetweenInterruption += summary.LatestUpTimeBetweenInterruption
			summary.InterruptionCount++
			summaryLog.V(5).Info("interruption event found, incrementing count")
		}
	}

	// Calculate means.
	if summary.InterruptionCount > 0 {
		summary.MeanUpTimeBetweenInterruption = summary.TotalUpTimeBetweenInterruption / time.Duration(summary.InterruptionCount)
	}
	if summary.RecoveryCount > 0 {
		summary.MeanDownTimeBetweenRecovery = summary.TotalDownTimeBetweenRecovery / time.Duration(summary.RecoveryCount)
	}

	// Add trailing up/interruption time.
	lastIdx := len(r.UpEvents) - 1
	if r.UpEvents[lastIdx].Up {
		summary.UpTime = summary.UpTime + now.Sub(r.UpEvents[lastIdx].Timestamp)
	} else {
		summary.DownTime = summary.DownTime + now.Sub(r.UpEvents[lastIdx].Timestamp)
	}

	summaryLog.V(1).Info("event summary", "summary", summary)
	return summary
}

func AppendUpEvent(now time.Time, rec *EventRecords, isUp bool) bool {
	var changed bool
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:        false,
			Timestamp: now,
		})
		changed = true
	}
	last := rec.UpEvents[len(rec.UpEvents)-1]
	if last.Up != isUp {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:        isUp,
			Timestamp: now,
		})
		changed = true
	}
	return changed
}

func ReconcileEvents(ctx context.Context, now time.Time, ups map[string]Upness, events map[string]EventRecords) bool {
	var changed bool

	reconcileLog := log.WithValues("event", ctx.Value(LogKey{}))
	for key, up := range ups {
		rec := events[key]
		reconcileLog.Info("ReconcileEvents", "key", key, "expected", up.ExpectedCount, "ready", up.ReadyCount, "unknown", up.UnknownCount)
		if AppendUpEvent(now, &rec, up.Up()) {
			events[key] = rec
			changed = true
		}
	}

	for key := range events {
		if _, ok := ups[key]; !ok {
			delete(events, key)
			changed = true
		}
	}

	return changed
}

package records

import (
	"context"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const JobSetRecordsAnnotationKey = "megamon.tbd/records"

type EventRecords struct {
	UpEvents []UpEvent `json:"upEvents"`
}

type UpEvent struct {
	Up           bool      `json:"up"`
	ExpectedDown bool      `json:"ed,omitempty"`
	Timestamp    time.Time `json:"ts"`
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
	summaryLog := logf.FromContext(ctx).WithName("events")

	n := len(r.UpEvents)
	summaryLog.V(3).Info("summarizing events", "event_count", n)
	if n == 0 {
		return summary
	}
	// Handle the case where the timeline starts with an Up event (e.g. attached to running system).
	if r.UpEvents[0].Up {
		summary.DownTimeInitial = 0
		if n == 1 {
			summary.UpTime = now.Sub(r.UpEvents[0].Timestamp)
			return summary
		}
	} else {
		// Standard case: Starts Down.
		if n == 1 {
			summary.DownTime = now.Sub(r.UpEvents[0].Timestamp)
			return summary
		}
	}

	// Validate second event based on the first event's state
	if r.UpEvents[0].Up {
		if r.UpEvents[1].Up {
			summaryLog.V(3).Info("invalid data: second event is up (but started up)")
			return summary
		}
	} else {
		if !r.UpEvents[1].Up {
			summaryLog.V(3).Info("invalid data: second event is not up (and started down)")
			return summary
		}
	}

	if !r.UpEvents[0].Up {
		summary.DownTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
		summary.DownTimeInitial = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	} else {
		// If we started Up, the first segment is UpTime.
		summary.UpTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	}
	// up:    ____      OR    up:        ____
	// down:      |____       down:  ____|
	// event: 0   1           event: 0   1
	if len(r.UpEvents) == 2 {
		if r.UpEvents[0].Up {
			// Case U -> D
			// UpTime was the duration of the first segment
			summary.UpTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)

			// Current state is Down
			summary.DownTime = now.Sub(r.UpEvents[1].Timestamp)
		} else {
			// Case D -> U (Standard)
			// UpTime is current duration
			summary.UpTime = now.Sub(r.UpEvents[1].Timestamp)
		}
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

			// Only increment interruption count if it's NOT expected downtime.
			if !r.UpEvents[i].ExpectedDown {
				summary.InterruptionCount++
				summaryLog.V(5).Info("interruption event found, incrementing count")
			} else {
				summaryLog.V(5).Info("expected downtime event found, skipping interruption count")
			}
		}
	}

	// Calculate means.
	if summary.InterruptionCount > 0 {
		summary.MeanUpTimeBetweenInterruption = summary.TotalUpTimeBetweenInterruption / time.Duration(summary.InterruptionCount)
	} else {
		// If there are no interruptions, semantically "Time Between Interruption" is undefined/zero.
		// We zero it out to avoid redundancy with UpTime and confusion.
		summary.TotalUpTimeBetweenInterruption = 0
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

func AppendUpEvent(now time.Time, rec *EventRecords, isUp bool, expectedDown bool) bool {
	var changed bool
	// If there are no events and the system is Up, we still record it to ensure we capture the state.
	// This ensures consistency even if it implies a fresh start for an existing workload.
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:           isUp,
			ExpectedDown: expectedDown,
			Timestamp:    now,
		})
		changed = true
		return changed
	}

	last := rec.UpEvents[len(rec.UpEvents)-1]
	if last.Up != isUp {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:           isUp,
			ExpectedDown: expectedDown,
			Timestamp:    now,
		})
		changed = true
	}
	return changed
}

func ReconcileEvents(ctx context.Context, now time.Time, ups map[string]Upness, events map[string]EventRecords, unknownThreshold float64) bool {
	var changed bool

	reconcileLog := logf.FromContext(ctx).WithName("events")

	for key, up := range ups {
		rec := events[key]
		reconcileLog.Info("ReconcileEvents", "key", key, "expected", up.ExpectedCount, "ready", up.ReadyCount, "unknownCount", up.UnknownCount, "unknownThreshold", unknownThreshold, "status", up.Status)

		isUp := up.Up(unknownThreshold)

		// If ExpectedDown is true (e.g. JobSet Completed), force isUp to false.
		// This ensures we stop the UpTime clock and record a Down event (Expected),
		// regardless of whether the Pod counts (Ready vs Expected) technically imply Up.
		if up.ExpectedDown {
			isUp = false
		}

		if AppendUpEvent(now, &rec, isUp, up.ExpectedDown) {
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

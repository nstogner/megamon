package records

import (
	"context"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const JobSetRecordsAnnotationKey = "megamon.tbd/records"

type EventRecords struct {
	UpEvents []UpEvent `json:"upEvents"`
	Attrs    Attrs     `json:"attrs,omitempty"`
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
	// (Down events with ExpectedDown=true don't increment InterruptionCount)
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
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:           false,
			ExpectedDown: expectedDown,
			Timestamp:    now,
		})
		changed = true
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

func ReconcileEvents(ctx context.Context, now time.Time, ups map[string]Upness, events map[string]EventRecords, unknownThreshold float64, gracePeriod time.Duration) bool {
	var changed bool

	reconcileLog := logf.FromContext(ctx).WithName("events")

	for key, up := range ups {
		rec := events[key]
		reconcileLog.Info("ReconcileEvents", "key", key, "expected", up.ExpectedCount, "ready", up.ReadyCount, "unknownCount", up.UnknownCount, "unknownThreshold", unknownThreshold, "status", up.Status, "gracePeriod", gracePeriod)

		isUp := up.Up(unknownThreshold)

		// If ExpectedDown is true (e.g. JobSet Completed), force isUp to false.
		// This ensures we stop the UpTime clock and record a Down event (Expected),
		// regardless of whether the Pod counts (Ready vs Expected) technically imply Up.
		if up.ExpectedDown {
			isUp = false
		}

		// Update attributes if they have changed. This ensures we have the latest
		// metadata (like slice state or owner info) even if the upness status hasn't changed.
		if rec.Attrs != up.Attrs {
			reconcileLog.V(5).Info("rec.Attrs != up.Attrs, updating", "rec.Attrs", rec.Attrs, "upAttrs", up.Attrs)
			rec.Attrs = up.Attrs // event record updates to match live state
			changed = true
			events[key] = rec
		}

		// Append a new up/down event if the state has transitioned.
		if AppendUpEvent(now, &rec, isUp, up.ExpectedDown) {
			events[key] = rec
			changed = true
		}
	}

	// Handle items that are no longer present in the current state.
	for key, rec := range events {
		if _, ok := ups[key]; !ok {
			// Delay deletion during grace period (e.g. slice repair) to maintain continuous history.
			if gracePeriod > 0 && len(rec.UpEvents) > 0 {
				lastEvent := rec.UpEvents[len(rec.UpEvents)-1]
				// If the item has been missing for less than the grace period, keep its records.
				if now.Sub(lastEvent.Timestamp) < gracePeriod {
					// Record the item as 'down' if its last known state was 'up'.
					if AppendUpEvent(now, &rec, false, false) {
						events[key] = rec
						changed = true
					}
					continue
				}
			}
			// Grace period has expired or is not configured, proceed with pruning the record.
			delete(events, key)
			changed = true
		}
	}

	return changed
}

// Package records defines the data structures and logic for tracking resource "upness" over time.
// This file specifically handles the management of historical EventRecords, which store a sequence
// of state change events (Up/Down) for a given resource. It provides the core logic for
// reconciling current observations against historical data to detect interruptions and recoveries.
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

const (
	ProvisioningStateProvisioning = "provisioning"
	ProvisioningStateSuccess      = "success"
	ProvisioningStateFailed       = "failed"
)

type UpEvent struct {
	Up           bool      `json:"up"`
	ExpectedDown bool      `json:"ed,omitempty"`
	Failed       bool      `json:"f,omitempty"`
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

	// ProvisioningDuration is the time spent provisioning.
	ProvisioningDuration time.Duration `json:"provisioningDuration"`
	// ProvisioningState is the state of provisioning.
	ProvisioningState string `json:"provisioningState"`
}

func (r *EventRecords) Summarize(ctx context.Context, now time.Time) EventSummary {
	var summary EventSummary
	log := logf.FromContext(ctx).WithName("events")

	n := len(r.UpEvents)
	log.V(3).Info("summarizing events", "event_count", n)
	if n == 0 {
		return summary
	}
	if r.UpEvents[0].Up {
		// Invalid data.
		log.V(3).Info("invalid data: first event is up")
		return summary
	}
	if n == 1 {
		summary.DownTime = now.Sub(r.UpEvents[0].Timestamp)
		summary.ProvisioningDuration = summary.DownTime
		summary.ProvisioningState = ProvisioningStateProvisioning
		return summary
	}
	// Invalid or missing data:
	if !r.UpEvents[1].Up {
		if r.UpEvents[1].Failed {
			summary.DownTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
			summary.DownTimeInitial = summary.DownTime
			summary.ProvisioningDuration = summary.DownTimeInitial
			summary.ProvisioningState = ProvisioningStateFailed
			return summary
		}
		log.V(3).Info("invalid data: second event is not up and not failed")
		return summary
	}

	summary.DownTime = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	summary.DownTimeInitial = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	summary.ProvisioningDuration = summary.DownTimeInitial
	summary.ProvisioningState = ProvisioningStateSuccess

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
			// Only increment recovery count if it's NOT recovering from expected downtime.
			if !r.UpEvents[i-1].ExpectedDown {
				summary.RecoveryCount++
				log.V(5).Info("recovery event found, incrementing count")
			} else {
				log.Info("WARNING: unexpected recovery from expected downtime event found", "upEvents", r.UpEvents)
			}
		} else {
			// Just transitioned up to down.
			summary.LatestUpTimeBetweenInterruption = r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.UpTime += summary.LatestUpTimeBetweenInterruption
			summary.TotalUpTimeBetweenInterruption += summary.LatestUpTimeBetweenInterruption

			// Only increment interruption count if it's NOT expected downtime.
			if !r.UpEvents[i].ExpectedDown {
				summary.InterruptionCount++
				log.V(5).Info("interruption event found, incrementing count")
			} else {
				log.V(5).Info("expected downtime event found, skipping interruption count")
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

	log.V(1).Info("event summary", "summary", summary)
	return summary
}

func AppendUpEvent(now time.Time, rec *EventRecords, isUp bool, expectedDown bool, failed bool) bool {
	var changed bool
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:           false,
			ExpectedDown: expectedDown,
			Failed:       failed,
			Timestamp:    now,
		})
		changed = true
	}

	last := rec.UpEvents[len(rec.UpEvents)-1]
	if last.Up != isUp || last.Failed != failed || last.ExpectedDown != expectedDown {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:           isUp,
			ExpectedDown: expectedDown,
			Failed:       failed,
			Timestamp:    now,
		})
		changed = true
	}
	return changed
}

func AppendStateChangeEvents(ctx context.Context, now time.Time, ups map[string]Upness, events map[string]EventRecords, unknownThreshold float64) bool {
	var changed bool

	log := logf.FromContext(ctx).WithName("events")

	for key, up := range ups {
		rec := events[key]
		log.Info("AppendStateChangeEvents", "key", key, "expected", up.ExpectedCount, "ready", up.ReadyCount, "unknownCount", up.UnknownCount, "unknownThreshold", unknownThreshold, "status", up.Status, "failed", up.Failed)

		isUp := up.Up(unknownThreshold)

		// If ExpectedDown is true (e.g. JobSet Completed), force isUp to false.
		// This ensures we stop the UpTime clock and record a Down event (Expected),
		// regardless of whether the Pod counts (Ready vs Expected) technically imply Up.
		if up.ExpectedDown {
			isUp = false
		}

		if AppendUpEvent(now, &rec, isUp, up.ExpectedDown, up.Failed) {
			changed = true
		}
		events[key] = rec
	}

	// Handle items that are no longer present in the current state.
	for key := range events {
		if _, ok := ups[key]; !ok {
			log.Info("deleting metric", "key", key)
			delete(events, key)
			changed = true
		}
	}

	return changed
}

func (r EventRecords) Clone() EventRecords {
	if r.UpEvents == nil {
		return EventRecords{}
	}
	upEventsCp := make([]UpEvent, len(r.UpEvents))
	copy(upEventsCp, r.UpEvents)
	return EventRecords{
		UpEvents: upEventsCp,
	}
}

func CloneEventRecordsMap(original map[string]EventRecords) map[string]EventRecords {
	if original == nil {
		return nil
	}
	cp := make(map[string]EventRecords, len(original))
	for k, v := range original {
		cp[k] = v.Clone()
	}
	return cp
}

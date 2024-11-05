package records

import (
	"time"
)

const JobSetRecordsAnnotationKey = "megamon.tbd/records"

type EventRecords struct {
	UpEvents []UpEvent `json:"upEvents"`
}

type UpEvent struct {
	Up        bool      `json:"up"`
	Timestamp time.Time `json:"timestamp"`
}

type UpnessSummaryWithAttrs struct {
	Attrs
	EventSummary
}

type EventSummary struct {
	// TimeBeforeUp is the time spent before the system was up.
	TimeBeforeUp time.Duration `json:"timeBeforeUp"`

	// InterruptionCount is the number of times that the system has gone down after being up.
	InterruptionCount int `json:"interruptionCount"`
	// RecoveryCount is the number of times that the system has recovered from a down state.
	RecoveryCount int `json:"recoveryCount"`

	// InterruptionTime is the total time spent in the down state after initially up.
	InterruptionTime time.Duration `json:"interruptionTime"`
	// UpTime is the total time spent in an up state.
	UpTime time.Duration `json:"upTime"`

	// TTTR - Total Time To Recovery
	TTTR time.Duration `json:"tttr"`
	// TTBI - Total Time Between Interruption
	TTBI time.Duration `json:"ttbi"`

	// LTTR - Last Time To Recovery
	LTTR time.Duration `json:"lttr"`
	// LTBI - Last Time Between Interruption
	LTBI time.Duration `json:"ltbi"`

	// MTTR - Mean Time To Recovery
	MTTR time.Duration `json:"mttr"`
	// MTBI - Mean Time Between Interruption
	MTBI time.Duration `json:"mbti"`
}

func (r *EventRecords) Summarize(now time.Time) EventSummary {
	var summary EventSummary

	if len(r.UpEvents) < 2 {
		return summary
	}

	// Invalid or missing data:
	if r.UpEvents[0].Up {
		return summary
	}
	if !r.UpEvents[1].Up {
		return summary
	}

	summary.TimeBeforeUp = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	if len(r.UpEvents) == 2 {
		summary.UpTime = now.Sub(r.UpEvents[1].Timestamp)
		return summary
	}

	var lastTBI, lastTTR time.Duration
	for i := 2; i < len(r.UpEvents); i++ {
		if r.UpEvents[i].Up {
			// Just transitioned down to up.
			lastTTR = r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.InterruptionTime += lastTTR
			summary.RecoveryCount++
		} else {
			// Just transitioned up to down.
			lastTBI = r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.UpTime += lastTBI
			summary.InterruptionCount++
		}
	}
	// Set values for last and total-"between" times.
	summary.LTBI = lastTBI
	summary.LTTR = lastTTR
	summary.TTBI = summary.UpTime
	summary.TTTR = summary.InterruptionTime

	// Calculate means.
	if summary.InterruptionCount > 0 {
		summary.MTBI = summary.TTBI / time.Duration(summary.InterruptionCount)
	}
	if summary.RecoveryCount > 0 {
		summary.MTTR = summary.TTTR / time.Duration(summary.RecoveryCount)
	}

	// Add trailing up/interruption time.
	lastIdx := len(r.UpEvents) - 1
	if r.UpEvents[lastIdx].Up {
		summary.UpTime = summary.TTBI + now.Sub(r.UpEvents[lastIdx].Timestamp)
	} else {
		summary.InterruptionTime = summary.TTTR + now.Sub(r.UpEvents[lastIdx].Timestamp)
	}

	return summary
}

func AppendUpEvent(rec *EventRecords, isUp bool) bool {
	var changed bool
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, UpEvent{
			Up:        isUp,
			Timestamp: time.Now(),
		})
		changed = true
	} else {
		last := rec.UpEvents[len(rec.UpEvents)-1]
		if last.Up != isUp {
			rec.UpEvents = append(rec.UpEvents, UpEvent{
				Up:        isUp,
				Timestamp: time.Now(),
			})
			changed = true
		}
	}
	return changed
}

/*
type Mode string

const (
	JobSetMode      Mode = "JobSet"
	JobSetNodesMode Mode = "JobSetNodes"
	NodePoolsMode   Mode = "NodePools"
)
*/

func ReconcileEvents( /*mode Mode,*/ ups map[string]Upness, events map[string]EventRecords) (bool, error) {
	var changed bool

	for key, up := range ups {
		/*
			var key string
			switch mode {
			case JobSetNodesMode, JobSetMode:
				key = jobsetEventKey(up.JobSetNamespace, up.JobSetName)
			case NodePoolsMode:
				key = up.NodePoolName
			default:
				panic("unsupported mode: " + mode)
			}
		*/

		rec := events[key]
		if AppendUpEvent(&rec, up.Up()) {
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

	return changed, nil
}

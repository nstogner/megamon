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
	TTIUp time.Duration `json:"ttiUp"`

	InterruptionTime time.Duration `json:"interruptionTime"`
	UpTime           time.Duration `json:"upTime"`

	Interruptions int `json:"interruptions"`
	Recoveries    int `json:"recoveries"`

	MTTR time.Duration `json:"mttr"`
	MTBI time.Duration `json:"mtbi"`
}

func (r *EventRecords) Summarize(now time.Time) EventSummary {
	var summary EventSummary

	if len(r.UpEvents) < 2 {
		return summary
	}

	// Invalid or missing data:
	if r.UpEvents[0].Up == true {
		return summary
	}
	if r.UpEvents[1].Up == false {
		return summary
	}

	summary.TTIUp = r.UpEvents[1].Timestamp.Sub(r.UpEvents[0].Timestamp)
	if len(r.UpEvents) == 2 {
		summary.UpTime = now.Sub(r.UpEvents[1].Timestamp)
		return summary
	}

	for i := 2; i < len(r.UpEvents); i++ {
		if r.UpEvents[i].Up {
			// Just transitioned down to up.
			summary.InterruptionTime += r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.Recoveries++
		} else {
			// Just transitioned up to down.
			summary.UpTime += r.UpEvents[i].Timestamp.Sub(r.UpEvents[i-1].Timestamp)
			summary.Interruptions++
		}
	}

	if summary.Interruptions > 0 {
		summary.MTBI = summary.UpTime / time.Duration(summary.Interruptions)
	}
	if summary.Recoveries > 0 {
		summary.MTTR = summary.InterruptionTime / time.Duration(summary.Recoveries)
	}

	// Add trailing uptime or interruption.
	lastIdx := len(r.UpEvents) - 1
	if r.UpEvents[lastIdx].Up {
		summary.UpTime += now.Sub(r.UpEvents[lastIdx].Timestamp)
	} else {
		summary.InterruptionTime += now.Sub(r.UpEvents[lastIdx].Timestamp)
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

	return changed, nil
}

func jobsetEventKey(namespace, name string) string {
	return namespace + "." + name
}

package records

import (
	"time"
)

const JobSetRecordsAnnotationKey = "megamon.tbd/records"

type JobSetMetadataRecords struct {
	UpEvents []JobSetUpEvent `json:"upEvents"`
}

type JobSetUpEvent struct {
	Up        bool      `json:"up"`
	Timestamp time.Time `json:"timestamp"`
}

type JobSetUpSummaryWithAttrs struct {
	JobSetAttrs
	JobSetUpSummary
}

type JobSetUpSummary struct {
	TTIUp time.Duration

	InterruptionTime time.Duration
	UpTime           time.Duration

	Interruptions int
	Recoveries    int

	MTTR time.Duration
	MTBI time.Duration
}

func (r *JobSetMetadataRecords) Summarize(now time.Time) JobSetUpSummary {
	var summary JobSetUpSummary

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

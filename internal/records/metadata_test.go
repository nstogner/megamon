package records

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJobSetUpSummary(t *testing.T) {
	t0, err := time.Parse(time.RFC3339, "2021-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		records         JobSetMetadataRecords
		now             time.Time
		expectedSummary JobSetUpSummary
	}{
		"empty": {
			records:         JobSetMetadataRecords{},
			expectedSummary: JobSetUpSummary{},
		},
		"missing down0": {
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: true, Timestamp: t0},
				},
			},
			expectedSummary: JobSetUpSummary{},
		},
		"just up": {
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					{Up: true, Timestamp: t0.Add(time.Hour)},
				},
			},
			now: t0.Add(time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp: time.Hour,
			},
		},
		"up for 3 hours": {
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					{Up: true, Timestamp: t0.Add(time.Hour)},
				},
			},
			now: t0.Add(time.Hour + 3*time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:  time.Hour,
				UpTime: 3 * time.Hour,
			},
		},
		"single interruption": {
			records: JobSetMetadataRecords{
				// up:         _____
				// down:   ____|   |
				// event:  0   1   2
				// hrs:      1   1
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
				},
			},
			now: t0.Add(2 * time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:         time.Hour,
				UpTime:        time.Hour,
				Interruptions: 1,
				MTBI:          time.Hour,
			},
		},
		"single interruption then down for an hour": {
			records: JobSetMetadataRecords{
				// up:         _____
				// down:   ____|   |____
				// event:  0   1   2
				// hrs:      1   1   1
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
				},
			},
			now: t0.Add(2*time.Hour + time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:            time.Hour,
				UpTime:           time.Hour,
				InterruptionTime: time.Hour,
				Interruptions:    1,
				MTBI:             time.Hour,
			},
		},
		"single interruption single recovery": {
			// up:         _____
			// down:   ____|   |___|
			// event:  0   1   2   3
			// hrs:      1   1   1
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 hr of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
					// 1 hr of downtime before recovery 0
					{Up: true, Timestamp: t0.Add(3 * time.Hour)},
				},
			},
			now: t0.Add(3 * time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:            time.Hour,
				UpTime:           time.Hour,
				InterruptionTime: time.Hour,
				Interruptions:    1,
				Recoveries:       1,
				MTTR:             time.Hour,
				MTBI:             time.Hour,
			},
		},
		"single interruption single recovery then up for an hour": {
			// up:         _____   _____
			// down:   ____|   |___|
			// event:  0   1   2   3
			// hrs:      1   1   1   1
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 hr of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
					// 1 hr of downtime before recovery 0
					{Up: true, Timestamp: t0.Add(3 * time.Hour)},
				},
			},
			now: t0.Add(3*time.Hour + time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:            time.Hour,
				UpTime:           2 * time.Hour,
				InterruptionTime: time.Hour,
				Interruptions:    1,
				Recoveries:       1,
				MTTR:             time.Hour,
				MTBI:             time.Hour,
			},
		},
		"two interruptions single recovery": {
			// up:         _____   _____
			// down:   ____|   |___|   |
			// event:  0   1   2   3   4
			// hrs:      1   1   1   2
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 hr of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
					// 1 hr of downtime before recovery 0
					{Up: true, Timestamp: t0.Add(3 * time.Hour)},
					// 2 hrs of uptime before interruption 1
					{Up: false, Timestamp: t0.Add(3*time.Hour + 2*time.Hour)},
				},
			},
			now: t0.Add(3*time.Hour + 2*time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:            time.Hour,
				UpTime:           time.Hour + 2*time.Hour,
				InterruptionTime: time.Hour,
				Interruptions:    2,
				Recoveries:       1,
				MTTR:             time.Hour,
				MTBI:             3 * time.Hour / 2,
			},
		},
		"two interruptions two recoveries": {
			// up:         _____   _____
			// down:   ____|   |___|   |___|
			// event:  0   1   2   3   4   5
			// hrs:      1   1   1   2   3
			records: JobSetMetadataRecords{
				UpEvents: []JobSetUpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 hr of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
					// 1 hr of downtime before recovery 0
					{Up: true, Timestamp: t0.Add(3 * time.Hour)},
					// 2 hrs of uptime before interruption 1
					{Up: false, Timestamp: t0.Add(3*time.Hour + 2*time.Hour)},
					// 3 hrs of downtime before recovery 1
					{Up: true, Timestamp: t0.Add(3*time.Hour + 2*time.Hour + 3*time.Hour)},
				},
			},
			now: t0.Add(3*time.Hour + 2*time.Hour + 3*time.Hour),
			expectedSummary: JobSetUpSummary{
				TTIUp:            time.Hour,
				UpTime:           time.Hour + 2*time.Hour,
				InterruptionTime: time.Hour + 3*time.Hour,
				Interruptions:    2,
				Recoveries:       2,
				MTTR:             2 * time.Hour,
				MTBI:             3 * time.Hour / 2,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotSum := tc.records.Summarize(tc.now)
			require.Equal(t, tc.expectedSummary.UpTime, gotSum.UpTime, "UpTime")
			require.Equal(t, tc.expectedSummary.InterruptionTime, gotSum.InterruptionTime, "InterruptionTime")
			require.Equal(t, tc.expectedSummary.Interruptions, gotSum.Interruptions, "Interruptions")
			require.Equal(t, tc.expectedSummary.Recoveries, gotSum.Recoveries, "Recoveries")
			require.Equal(t, tc.expectedSummary.MTTR, gotSum.MTTR, "MTTR")
			require.Equal(t, tc.expectedSummary.MTBI, gotSum.MTBI, "MTBI")
		})
	}
}

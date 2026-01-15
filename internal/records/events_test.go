package records

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	t0, err := time.Parse(time.RFC3339, "2021-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		records         EventRecords
		now             time.Time
		expectedSummary EventSummary
	}{
		"empty": {
			records:         EventRecords{},
			expectedSummary: EventSummary{},
		},
		"missing down0": {
			records: EventRecords{
				UpEvents: []UpEvent{
					{Up: true, Timestamp: t0},
				},
			},
			expectedSummary: EventSummary{},
		},
		"not up yet": {
			records: EventRecords{
				// up:
				// down:   _____
				// event:  0
				// hrs:      1
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
				},
			},
			now: t0.Add(time.Hour),
			expectedSummary: EventSummary{
				DownTime: time.Hour,
			},
		},
		"just up": {
			records: EventRecords{
				// up:
				// down:   ____|
				// event:  0   1
				// hrs:      1
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
					{Up: true, Timestamp: t0.Add(time.Hour)},
				},
			},
			now: t0.Add(time.Hour),
			expectedSummary: EventSummary{
				DownTime:        time.Hour,
				DownTimeInitial: time.Hour,
			},
		},
		"up for 3 hours": {
			records: EventRecords{
				// up:         _____
				// down:   ____|
				// event:  0   1   2
				// hrs:      1   3
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
					{Up: true, Timestamp: t0.Add(time.Hour)},
				},
			},
			now: t0.Add(time.Hour + 3*time.Hour),
			expectedSummary: EventSummary{
				DownTimeInitial: time.Hour,
				DownTime:        time.Hour,
				UpTime:          3 * time.Hour,
			},
		},
		"single interruption": {
			records: EventRecords{
				// up:         _____
				// down:   ____|   |
				// event:  0   1   2
				// hrs:      1   1
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
				},
			},
			now: t0.Add(2 * time.Hour),
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour,
				DownTime:                        time.Hour,
				InterruptionCount:               1,
				TotalUpTimeBetweenInterruption:  time.Hour,
				MeanUpTimeBetweenInterruption:   time.Hour,
				LatestUpTimeBetweenInterruption: time.Hour,
			},
		},
		"single interruption then down for an hour": {
			records: EventRecords{
				// up:         _____
				// down:   ____|   |____
				// event:  0   1   2
				// hrs:      1   1   1
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 of uptime before interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour)},
				},
			},
			now: t0.Add(2*time.Hour + time.Hour),
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour,
				DownTime:                        2 * time.Hour,
				InterruptionCount:               1,
				TotalUpTimeBetweenInterruption:  time.Hour,
				MeanUpTimeBetweenInterruption:   time.Hour,
				LatestUpTimeBetweenInterruption: time.Hour,
			},
		},
		"single interruption single recovery": {
			// up:         _____
			// down:   ____|   |___|
			// event:  0   1   2   3
			// hrs:      1   1   1
			records: EventRecords{
				UpEvents: []UpEvent{
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
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour,
				DownTime:                        2 * time.Hour,
				InterruptionCount:               1,
				RecoveryCount:                   1,
				TotalDownTimeBetweenRecovery:    time.Hour,
				MeanDownTimeBetweenRecovery:     time.Hour,
				LatestDownTimeBetweenRecovery:   time.Hour,
				TotalUpTimeBetweenInterruption:  time.Hour,
				MeanUpTimeBetweenInterruption:   time.Hour,
				LatestUpTimeBetweenInterruption: time.Hour,
			},
		},
		"single interruption single recovery then up for an hour": {
			// up:         _____   _____
			// down:   ____|   |___|
			// event:  0   1   2   3
			// hrs:      1   1   1   1
			records: EventRecords{
				UpEvents: []UpEvent{
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
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          2 * time.Hour,
				DownTime:                        2 * time.Hour,
				InterruptionCount:               1,
				RecoveryCount:                   1,
				TotalDownTimeBetweenRecovery:    time.Hour,
				MeanDownTimeBetweenRecovery:     time.Hour,
				LatestDownTimeBetweenRecovery:   time.Hour,
				TotalUpTimeBetweenInterruption:  time.Hour,
				MeanUpTimeBetweenInterruption:   time.Hour,
				LatestUpTimeBetweenInterruption: time.Hour,
			},
		},
		"two interruptions single recovery": {
			// up:         _____   _____
			// down:   ____|   |___|   |
			// event:  0   1   2   3   4
			// hrs:      1   1   1   2
			records: EventRecords{
				UpEvents: []UpEvent{
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
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour + 2*time.Hour,
				DownTime:                        2 * time.Hour,
				InterruptionCount:               2,
				RecoveryCount:                   1,
				TotalDownTimeBetweenRecovery:    time.Hour,
				MeanDownTimeBetweenRecovery:     time.Hour,
				LatestDownTimeBetweenRecovery:   time.Hour,
				TotalUpTimeBetweenInterruption:  1*time.Hour + 2*time.Hour,
				MeanUpTimeBetweenInterruption:   (1*time.Hour + 2*time.Hour) / 2,
				LatestUpTimeBetweenInterruption: 2 * time.Hour,
			},
		},
		"two interruptions one recovery": {
			// up:         _____   _____
			// down:   ____|   |___|   |___
			// event:  0   1   2   3   4
			// hrs:      1   1   1   2   3
			records: EventRecords{
				UpEvents: []UpEvent{
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
			now: t0.Add(3*time.Hour + 2*time.Hour + 3*time.Hour),
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour + 2*time.Hour,
				DownTime:                        time.Hour + time.Hour + 3*time.Hour,
				InterruptionCount:               2,
				RecoveryCount:                   1,
				TotalDownTimeBetweenRecovery:    1 * time.Hour,
				MeanDownTimeBetweenRecovery:     1 * time.Hour,
				LatestDownTimeBetweenRecovery:   1 * time.Hour,
				TotalUpTimeBetweenInterruption:  1*time.Hour + 2*time.Hour,
				MeanUpTimeBetweenInterruption:   (1*time.Hour + 2*time.Hour) / 2,
				LatestUpTimeBetweenInterruption: 2 * time.Hour,
			},
		},
		"two interruptions two recoveries - different durations": {
			// up:         _____   ______
			// down:   ____|   |___|    |______|
			// event:  0   1   2   3    4      5
			// hrs:      1   1   1    2     3
			records: EventRecords{
				UpEvents: []UpEvent{
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
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour + 2*time.Hour,
				DownTime:                        time.Hour + time.Hour + 3*time.Hour,
				InterruptionCount:               2,
				RecoveryCount:                   2,
				TotalDownTimeBetweenRecovery:    time.Hour + 3*time.Hour,
				MeanDownTimeBetweenRecovery:     2 * time.Hour,
				LatestDownTimeBetweenRecovery:   3 * time.Hour,
				TotalUpTimeBetweenInterruption:  1*time.Hour + 2*time.Hour,
				MeanUpTimeBetweenInterruption:   (1*time.Hour + 2*time.Hour) / 2,
				LatestUpTimeBetweenInterruption: 2 * time.Hour,
			},
		},
		// Error cases
		"up0": {
			records: EventRecords{
				// up:     _____
				// down:
				// event:  0
				// hrs:      1
				UpEvents: []UpEvent{
					{Up: true, Timestamp: t0},
				},
			},
			now:             t0.Add(time.Hour),
			expectedSummary: EventSummary{},
		},
		"up0 then down1": {
			records: EventRecords{
				// up:     _____
				// down:       |
				// event:  0
				// hrs:      1
				UpEvents: []UpEvent{
					{Up: true, Timestamp: t0},
					{Up: false, Timestamp: t0.Add(time.Hour)},
				},
			},
			now:             t0.Add(time.Hour),
			expectedSummary: EventSummary{},
		},
		"no events": {
			records: EventRecords{
				UpEvents: []UpEvent{},
			},
			now:             t0.Add(time.Hour),
			expectedSummary: EventSummary{},
		},
		"expected downtime interruption": {
			records: EventRecords{
				// up:         _____
				// down:   ____|   |
				// event:  0   1   2
				// hrs:      1   1
				UpEvents: []UpEvent{
					{Up: false, Timestamp: t0},
					// 1 hr to come up
					{Up: true, Timestamp: t0.Add(time.Hour)},
					// 1 of uptime before EXPECTED interruption 0
					{Up: false, Timestamp: t0.Add(2 * time.Hour), ExpectedDown: true},
				},
			},
			now: t0.Add(2 * time.Hour),
			expectedSummary: EventSummary{
				DownTimeInitial:                 time.Hour,
				UpTime:                          time.Hour,
				DownTime:                        time.Hour,
				InterruptionCount:               0, // Should be 0 because it's expected
				TotalUpTimeBetweenInterruption:  time.Hour,
				MeanUpTimeBetweenInterruption:   0, // No interruptions
				LatestUpTimeBetweenInterruption: time.Hour,
			},
		},
	}

	ctx := context.Background()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotSum := tc.records.Summarize(ctx, tc.now)
			require.Equal(t, tc.expectedSummary.UpTime, gotSum.UpTime, "UpTime")
			require.Equal(t, tc.expectedSummary.DownTime, gotSum.DownTime, "DownTime")
			require.Equal(t, tc.expectedSummary.DownTimeInitial, gotSum.DownTimeInitial, "DownTimeInitial")
			require.Equal(t, tc.expectedSummary.InterruptionCount, gotSum.InterruptionCount, "InterruptionCount")
			require.Equal(t, tc.expectedSummary.RecoveryCount, gotSum.RecoveryCount, "RecoveryCount")
			require.Equal(t, tc.expectedSummary.TotalDownTimeBetweenRecovery, gotSum.TotalDownTimeBetweenRecovery, "TotalDownTimeBetweenRecovery")
			require.Equal(t, tc.expectedSummary.MeanDownTimeBetweenRecovery, gotSum.MeanDownTimeBetweenRecovery, "MeanDownTimeBetweenRecovery")
			require.Equal(t, tc.expectedSummary.LatestDownTimeBetweenRecovery, gotSum.LatestDownTimeBetweenRecovery, "LatestDownTimeBetweenRecovery")
			require.Equal(t, tc.expectedSummary.TotalUpTimeBetweenInterruption, gotSum.TotalUpTimeBetweenInterruption, "TotalUpTimeBetweenInterruption")
			require.Equal(t, tc.expectedSummary.MeanUpTimeBetweenInterruption, gotSum.MeanUpTimeBetweenInterruption, "MeanUpTimeBetweenInterruption")
			require.Equal(t, tc.expectedSummary.LatestUpTimeBetweenInterruption, gotSum.LatestUpTimeBetweenInterruption, "LatestUpTimeBetweenInterruption")
		})
	}
}

func TestReconcileEvents(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := map[string]struct {
		inputUps         map[string]Upness
		inputEvents      map[string]EventRecords
		expEvents        map[string]EventRecords
		expChanged       bool
		unknownThreshold float64
	}{
		"empty": {
			inputUps:         map[string]Upness{},
			inputEvents:      map[string]EventRecords{},
			expEvents:        map[string]EventRecords{},
			unknownThreshold: 1.0,
			expChanged:       false,
		},
		"first event up": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    1,
				},
			},
			inputEvents: map[string]EventRecords{},
			// The first event is Up. We intentionally do not record an initial Up event
			// if we have no prior history, as we assume it started healthy.
			// This avoids assuming a prior Down state.
			expEvents:        map[string]EventRecords{},
			unknownThreshold: 1.0,
			expChanged:       false,
		},
		"first event down": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    0,
				},
			},
			inputEvents: map[string]EventRecords{},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       true,
		},
		"down to up 10 expected, 9 ready, 1 unknown unknown threshold 0.1": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 10,
					ReadyCount:    9,
					UnknownCount:  1,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
						{Up: true, Timestamp: now},
					},
				},
			},
			unknownThreshold: 0.1,
			expChanged:       true,
		},
		"stay down, 10 expected, 8 ready, 2 unknown unknown threshold 0.1": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 10,
					ReadyCount:    8,
					UnknownCount:  2,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now},
					},
				},
			},
			unknownThreshold: 0.1,
			expChanged:       false,
		},
		"expected downtime": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    0,
					ExpectedDown:  true,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: true, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: true, Timestamp: now.Add(-time.Minute)},
						{Up: false, Timestamp: now, ExpectedDown: true},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       true,
		},
		"unplanned downtime (failed/suspended)": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    0,
					ExpectedDown:  false,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: true, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: true, Timestamp: now.Add(-time.Minute)},
						{Up: false, Timestamp: now},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       true,
		},
		"down to up": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    1,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
						{Up: true, Timestamp: now},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       true,
		},
		"up to down": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    0,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-2 * time.Minute)},
						{Up: true, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-2 * time.Minute)},
						{Up: true, Timestamp: now.Add(-time.Minute)},
						{Up: false, Timestamp: now},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       true,
		},
		"still down": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    0,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       false,
		},
		"still up": {
			inputUps: map[string]Upness{
				"abc": {
					ExpectedCount: 1,
					ReadyCount:    1,
				},
			},
			inputEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-2 * time.Minute)},
						{Up: true, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			expEvents: map[string]EventRecords{
				"abc": {
					UpEvents: []UpEvent{
						{Up: false, Timestamp: now.Add(-2 * time.Minute)},
						{Up: true, Timestamp: now.Add(-time.Minute)},
					},
				},
			},
			unknownThreshold: 1.0,
			expChanged:       false,
		},
	}

	ctx := context.Background()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			events := c.inputEvents
			gotChanged := ReconcileEvents(ctx, now, c.inputUps, events, c.unknownThreshold)
			require.Equal(t, c.expEvents, events)
			require.Equal(t, c.expChanged, gotChanged)
		})
	}
}

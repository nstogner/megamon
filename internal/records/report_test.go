package records

import (
	"testing"

	"example.com/megamon/internal/experiments"
	"github.com/stretchr/testify/require"
)

// Helper to set experiment config for a test
func setupExperiment(t *testing.T, config map[string]experiments.ExperimentConfig) {
	// Reset experiments before each test case within a test function
	experiments.SetExperimentsConfig(config)
	t.Cleanup(func() {
		// Clean up experiments after the test case runs
		experiments.SetExperimentsConfig(map[string]experiments.ExperimentConfig{})
	})
}

func TestUpness_Up_NodeUnknownAsNotReady(t *testing.T) {
	tests := []struct {
		name           string
		upness         Upness
		experimentConf map[string]experiments.ExperimentConfig // Config for the experiment package
		expected       bool
	}{
		// --- Experiment Disabled ---
		{
			name: "NodeUnknownAsNotReady exp disabled - Ready == Expected",
			upness: Upness{
				ReadyCount:    10,
				ExpectedCount: 10,
				UnknownCount:  0,
			},
			experimentConf: map[string]experiments.ExperimentConfig{},
			expected:       true,
		},
		{
			name: "NodeUnknownAsNotReady exp disabled - Ready < Expected",
			upness: Upness{
				ReadyCount:    9,
				ExpectedCount: 10,
				UnknownCount:  1, // Unknown doesn't matter when experiment is off
			},
			experimentConf: map[string]experiments.ExperimentConfig{},
			expected:       false,
		},
		// --- Experiment Enabled ---
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, ready == expected",
			upness: Upness{
				ReadyCount:    10,
				ExpectedCount: 10,
				UnknownCount:  0,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, ready == expected, unknown count > 0",
			upness: Upness{
				ReadyCount:    10,
				ExpectedCount: 10,
				UnknownCount:  2,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 10 expected, 9 ready, 1 unknown",
			upness: Upness{
				ReadyCount:    9,
				ExpectedCount: 10,
				UnknownCount:  1,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 10 expected, 8 ready, 1 unknown",
			upness: Upness{
				ReadyCount:    8,
				ExpectedCount: 10,
				UnknownCount:  1,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: false,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 10 expected, 8 ready, 2 unknown",
			upness: Upness{
				ReadyCount:    8,
				ExpectedCount: 10,
				UnknownCount:  2,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: false,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 16 expected, 15 ready, 1 unknown",
			upness: Upness{
				ReadyCount:    15,
				ExpectedCount: 16,
				UnknownCount:  1,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 16 expected, 14 ready, 2 unknown",
			upness: Upness{
				ReadyCount:    14,
				ExpectedCount: 16,
				UnknownCount:  2,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 16 expected, 13 ready, 3 unknown",
			upness: Upness{
				ReadyCount:    13,
				ExpectedCount: 16,
				UnknownCount:  3,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: false,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 0.1, 16 expected, 14 ready, 1 unknown",
			upness: Upness{
				ReadyCount:    14,
				ExpectedCount: 16,
				UnknownCount:  1,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   0.1,
				},
			},
			expected: true,
		},
		{
			name: "NodeUnknownAsNotReady exp enabled at 1.0, 16 expected, 14 ready, 1 unknown",
			upness: Upness{
				ReadyCount:    8,
				ExpectedCount: 16,
				UnknownCount:  8,
			},
			experimentConf: map[string]experiments.ExperimentConfig{
				"NodeUnknownAsNotReady": {
					Enabled: true,
					Value:   1.0,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup experiment state for this specific test case
			setupExperiment(t, tt.experimentConf)
			got := tt.upness.Up()
			require.Equal(t, tt.expected, got)
		})
	}
}

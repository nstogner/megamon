package records

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpness_Up(t *testing.T) {
	tests := []struct {
		name             string
		upness           Upness
		expected         bool
		unknownThreshold float64
	}{
		{
			name: "Ready == Expected",
			upness: Upness{
				ReadyCount:    10,
				ExpectedCount: 10,
				UnknownCount:  0,
			},
			unknownThreshold: 1.0,
			expected:         true,
		},
		{
			name: "Ready+Unknown == Expected",
			upness: Upness{
				ReadyCount:    7,
				ExpectedCount: 10,
				UnknownCount:  3,
			},
			unknownThreshold: 1.0,
			expected:         true,
		},
		{
			name: "Ready 9, Unknown 1, threshold 0.1",
			upness: Upness{
				ReadyCount:    9,
				ExpectedCount: 10,
				UnknownCount:  1,
			},
			unknownThreshold: 0.1,
			expected:         true,
		},
		{
			name: "Ready 14, Unknown 2, threshold 0.1",
			upness: Upness{
				ReadyCount:    14,
				ExpectedCount: 16,
				UnknownCount:  2,
			},
			unknownThreshold: 0.1,
			expected:         true,
		},
		{
			name: "Ready 13, Unknown 3, threshold 0.1",
			upness: Upness{
				ReadyCount:    13,
				ExpectedCount: 16,
				UnknownCount:  3,
			},
			unknownThreshold: 0.1,
			expected:         false,
		},
		{
			name: "Ready+Unknown < Expected, threshold 0.5",
			upness: Upness{
				ReadyCount:    7,
				ExpectedCount: 10,
				UnknownCount:  2,
			},
			unknownThreshold: 0.5,
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.upness.Up(tt.unknownThreshold)
			require.Equal(t, tt.expected, got)
		})
	}
}

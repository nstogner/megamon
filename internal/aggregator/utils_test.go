package aggregator

import (
	"testing"

	"github.com/stretchr/testify/require"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
)

func TestGetExpectedTPUNodePoolSize(t *testing.T) {
	cases := map[string]struct {
		np              *containerv1beta1.NodePool
		want            int32
		wantErrContains string
	}{
		"empty": {
			np:              &containerv1beta1.NodePool{},
			wantErrContains: "no placement policy",
		},
		"v5e 16x16 4": {
			np: &containerv1beta1.NodePool{
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "16x16",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "ct5lp-hightpu-4t",
				},
			},
			want: 64,
		},
		"v5e 2x4 8": {
			np: &containerv1beta1.NodePool{
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "2x4",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "ct5lp-hightpu-8t",
				},
			},
			want: 1,
		},
		"v5e 2x4 4": {
			np: &containerv1beta1.NodePool{
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "2x4",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "ct5lp-hightpu-4t",
				},
			},
			want: 2,
		},
		"v5p 8x8x8 4": {
			np: &containerv1beta1.NodePool{
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "8x8x8",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "ct5p-hightpu-4t",
				},
			},
			want: 128,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := getExpectedTPUNodePoolSize(c.np)
			if c.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.wantErrContains)
			}
			require.Equal(t, c.want, got)
		})
	}
}

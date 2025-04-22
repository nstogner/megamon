package k8sutils_test

import (
	"testing"
	"time"

	"example.com/megamon/internal/k8sutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetExpectedTPUNodePoolSize(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		node            *corev1.Node
		want            int32
		wantErrContains string
	}{
		"empty": {
			node:            &corev1.Node{},
			wantErrContains: "no annotations",
		},
		"v5e 16x16 4": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cloud.google.com/gke-tpu-topology":      "16x16",
						"cloud.google.com/gke-accelerator-count": "4",
					},
				},
			},
			want: 64,
		},
		"v5e 2x4 8": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cloud.google.com/gke-tpu-topology":      "2x4",
						"cloud.google.com/gke-accelerator-count": "8",
					},
				},
			},
			want: 1,
		},
		"v5e 2x4 4": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cloud.google.com/gke-tpu-topology":      "2x4",
						"cloud.google.com/gke-accelerator-count": "4",
					},
				},
			},
			want: 2,
		},
		"v5p 8x8x8 4": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cloud.google.com/gke-tpu-topology":      "8x8x8",
						"cloud.google.com/gke-accelerator-count": "4",
					},
				},
			},
			want: 128,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := k8sutils.GetExpectedTPUNodePoolSize(c.node)
			if c.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.wantErrContains)
			}
			require.Equal(t, c.want, got)
		})
	}

}

func TestGetTpuTopologyToChipCount(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		topo            string
		want            int
		wantErrContains string
	}{
		"valid 2x2": {
			topo: "2x2",
			want: 4,
		},
		"valid 2x4": {
			topo: "2x4",
			want: 8,
		},
		"valid 4x2": {
			topo: "4x2",
			want: 8,
		},
		"valid 8x8x8": {
			topo: "8x8x8",
			want: 512,
		},
		"invalid empty": {
			topo:            "",
			wantErrContains: "invalid topology",
		},
		"invalid single": {
			topo:            "2",
			wantErrContains: "invalid topology",
		},
		"invalid 2x": {
			topo:            "2x",
			wantErrContains: "invalid topology",
		},
		"invalid x2": {
			topo:            "x2",
			wantErrContains: "invalid topology",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := k8sutils.GetTpuTopologyToChipCount(c.topo)
			if c.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.wantErrContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func nodeStatusBuilder(condType corev1.NodeConditionType, status corev1.ConditionStatus, lastTransitionTime time.Time) corev1.NodeStatus {
	return corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{
			{
				Type:               condType,
				Status:             status,
				LastTransitionTime: metav1.NewTime(lastTransitionTime),
			},
		},
	}
}
func nodeBuilder(condType corev1.NodeConditionType, status corev1.ConditionStatus, lastTransitionTime time.Time) *corev1.Node {
	return &corev1.Node{
		Status: nodeStatusBuilder(condType, status, lastTransitionTime),
	}
}

func TestIsNodeReady(t *testing.T) {
	cases := map[string]struct {
		node             *corev1.Node
		want             corev1.ConditionStatus
		unknownThreshold float64
	}{
		// unknownThreshold == 1.0 behavior (og megamon)
		"empty, unknownThreshold 1.0": {
			node:             &corev1.Node{},
			want:             corev1.ConditionUnknown,
			unknownThreshold: 1.0,
		},
		"ready, unknownThreshold 1.0": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionTrue, time.Now()),
			unknownThreshold: 1.0,
			want:             corev1.ConditionTrue,
		},
		"not ready, unknownThreshold 1.0": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionFalse, time.Now()),
			unknownThreshold: 1.0,
			want:             corev1.ConditionFalse,
		},
		"unknown, unknownThreshold 1.0": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionUnknown, time.Now()),
			unknownThreshold: 1.0,
			want:             corev1.ConditionTrue,
		},
		"unknown status older than 3 minutes, unknownThreshold 1.0": {
			node: &corev1.Node{
				Status: nodeStatusBuilder(corev1.NodeReady, corev1.ConditionUnknown, time.Now().Add(-5*time.Minute)),
			},
			unknownThreshold: 1.0,
			want:             corev1.ConditionUnknown,
		},
		// unknownThreshold == 0.1
		"unknownThreshold 0.1, unknown status older than 3 minutes with NodeUnknownAsReady enabled": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionUnknown, time.Now().Add(-5*time.Minute)),
			unknownThreshold: 0.1,
			want:             corev1.ConditionUnknown,
		},
		"unknownThreshold 0.1, unknown status with NodeUnknownAsReady enabled, last transition < 3 minutes": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionUnknown, time.Now().Add(-1*time.Minute)),
			unknownThreshold: 0.1,
			want:             corev1.ConditionUnknown,
		},
		"unknownThreshold 0.1 ready": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionTrue, time.Now()),
			unknownThreshold: 0.1,
			want:             corev1.ConditionTrue,
		},
		"unknownThreshold 0.1 not ready": {
			node:             nodeBuilder(corev1.NodeReady, corev1.ConditionFalse, time.Now()),
			unknownThreshold: 0.1,
			want:             corev1.ConditionFalse,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := k8sutils.IsNodeReady(c.node, c.unknownThreshold)
			require.Equal(t, c.want, got)
		})
	}
}

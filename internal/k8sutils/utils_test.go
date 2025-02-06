package k8sutils_test

import (
	"testing"

	"example.com/megamon/internal/k8sutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTpuTopologyToChipCount(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		topo          string
		want          int
		wantErrString string
	}{
		"empty": {
			topo:          "",
			wantErrString: "invalid topology",
		},
		"invalid topo": {
			topo:          "abc",
			wantErrString: "invalid topology",
		},

		"2x2": {
			topo: "2x2",
			want: 4,
		},
		"1x2x4": {
			topo: "1x2x4",
			want: 8,
		},
		"8x8x4": {
			topo: "8x8x4",
			want: 256,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := k8sutils.TpuTopologyToChipCount(c.topo)
			if c.wantErrString != "" {
				require.ErrorContains(t, err, c.wantErrString)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

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

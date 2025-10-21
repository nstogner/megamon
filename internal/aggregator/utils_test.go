package aggregator

import (
	"testing"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	"github.com/stretchr/testify/require"

	containerv1beta1 "google.golang.org/api/container/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
		"v7x 2x2x2": {
			np: &containerv1beta1.NodePool{
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "2x2x2",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "tpu7x-standard-4t",
				},
			},
			want: 2,
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

func TestExtractJobSetAttrs(t *testing.T) {
	cases := map[string]struct {
		js   *jobset.JobSet
		want records.Attrs
	}{
		"empty": {
			js:   &jobset.JobSet{},
			want: records.Attrs{},
		},
		"basic": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "test-ns",
					UID:       "12345",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											NodeSelector: map[string]string{
												k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
												k8sutils.NodeLabelGKETPUTopology:    "2x2x1",
												k8sutils.NodeLabelGKESpot:           "true",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: records.Attrs{
				JobSetName:      "test-jobset",
				JobSetNamespace: "test-ns",
				JobSetUID:       "12345",
				TPUAccelerator:  "tpu-v5p",
				TPUTopology:     "2x2x1",
				Spot:            true,
				TPUChipCount:    4,
			},
		},
		"multiple replicated jobs": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "test-ns",
					UID:       "12345",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											NodeSelector: map[string]string{
												k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
												k8sutils.NodeLabelGKETPUTopology:    "2x2x1",
												k8sutils.NodeLabelGKESpot:           "false",
											},
										},
									},
								},
							},
						},
						{
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											NodeSelector: map[string]string{
												k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
												k8sutils.NodeLabelGKETPUTopology:    "2x4x1",
												k8sutils.NodeLabelGKESpot:           "false",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: records.Attrs{
				JobSetName:      "test-jobset",
				JobSetNamespace: "test-ns",
				JobSetUID:       "12345",
				TPUAccelerator:  "tpu-v5p",
				TPUTopology:     "2x4x1",
				Spot:            false,
				TPUChipCount:    20,
			},
		},
		"missing topology": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "test-ns",
					UID:       "12345",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											NodeSelector: map[string]string{
												k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: records.Attrs{
				JobSetName:      "test-jobset",
				JobSetNamespace: "test-ns",
				JobSetUID:       "12345",
				TPUAccelerator:  "tpu-v5p",
				Spot:            false,
				TPUChipCount:    0,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := extractJobSetAttrs(c.js)
			require.Equal(t, c.want, got)
		})
	}
}

func TestExtractNodeAttrs(t *testing.T) {
	cases := map[string]struct {
		node *corev1.Node
		want records.Attrs
	}{
		"empty": {
			node: &corev1.Node{},
			want: records.Attrs{},
		},
		"basic": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
						k8sutils.NodeLabelGKETPUTopology:    "2x2x1",
						k8sutils.NodeLabelGKESpot:           "true",
					},
				},
			},
			want: records.Attrs{
				TPUAccelerator: "tpu-v5p",
				TPUTopology:    "2x2x1",
				Spot:           true,
			},
		},
		"missing optional fields": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
					},
				},
			},
			want: records.Attrs{
				TPUAccelerator: "tpu-v5p",
				Spot:           false,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := extractNodeAttrs(c.node)
			require.Equal(t, c.want, got)
		})
	}
}

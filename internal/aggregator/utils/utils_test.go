package utils

import (
	"testing"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	"github.com/stretchr/testify/require"

	slicev1beta1 "example.com/megamon/copied-slice-api/v1beta1"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"
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
			got, err := GetExpectedTPUNodePoolSize(c.np)
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
		"with topology block labels": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "test-ns",
					UID:       "12345",
					Labels: map[string]string{
						k8sutils.NodeLabelGKETopologyBlock:     "9a0e671424e45fd480ca172ad7a4e25d",
						k8sutils.NodeLabelGKEReservationBlocks: "tpu7x-test-block-0001",
					},
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
				JobSetName:           "test-jobset",
				JobSetNamespace:      "test-ns",
				JobSetUID:            "12345",
				TPUAccelerator:       "tpu-v5p",
				TPUTopology:          "2x2x1",
				Spot:                 true,
				TopologyBlockID:      "9a0e671424e45fd480ca172ad7a4e25d",
				ReservationBlockName: "tpu7x-test-block-0001",
				TPUChipCount:         4,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ExtractJobSetAttrs(c.js)
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
						k8sutils.NodeLabelGKETPUSlice:       "test-slice",
						k8sutils.NodeLabelGKESpot:           "true",
					},
				},
			},
			want: records.Attrs{
				TPUAccelerator: "tpu-v5p",
				TPUTopology:    "2x2x1",
				SliceName:      "test-slice",
				Spot:           true,
			},
		},
		"with topology block and subblock": {
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						k8sutils.NodeLabelGKETPUAccelerator:       "tpu-v5p",
						k8sutils.NodeLabelGKETPUTopology:          "2x2x1",
						k8sutils.NodeLabelGKETPUSlice:             "test-slice",
						k8sutils.NodeLabelGKESpot:                 "true",
						k8sutils.NodeLabelGKETopologyBlock:        "9a0e671424e45fd480ca172ad7a4e25d",
						k8sutils.NodeLabelGKETopologySubBlock:     "6ce4a464bd524e332477fad57c0875a5",
						k8sutils.NodeLabelGKEReservationBlocks:    "tpu7x-test-block-0001",
						k8sutils.NodeLabelGKEReservationSubBlocks: "tpu7x-test-block-0001-subblock-0002",
					},
				},
			},
			want: records.Attrs{
				TPUAccelerator:          "tpu-v5p",
				TPUTopology:             "2x2x1",
				SliceName:               "test-slice",
				Spot:                    true,
				TopologyBlockID:         "9a0e671424e45fd480ca172ad7a4e25d",
				TopologySubBlockID:      "6ce4a464bd524e332477fad57c0875a5",
				ReservationBlockName:    "tpu7x-test-block-0001",
				ReservationSubBlockName: "tpu7x-test-block-0001-subblock-0002",
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
			got := ExtractNodeAttrs(c.node)
			require.Equal(t, c.want, got)
		})
	}
}

func TestExtractNodePoolAttrs(t *testing.T) {
	cases := map[string]struct {
		np   *containerv1beta1.NodePool
		want records.Attrs
	}{
		"empty": {
			np:   &containerv1beta1.NodePool{},
			want: records.Attrs{},
		},
		"with topology and reservation labels": {
			np: &containerv1beta1.NodePool{
				Name: "test-nodepool",
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "2x2x2",
				},
				Config: &containerv1beta1.NodeConfig{
					Spot: true,
					ResourceLabels: map[string]string{
						k8sutils.NodePoolResourceLabelGKEAcceleratorType: "tpu7x",
					},
					Labels: map[string]string{
						k8sutils.NodeLabelGKETopologyBlock:        "9a0e671424e45fd480ca172ad7a4e25d",
						k8sutils.NodeLabelGKETopologySubBlock:     "6ce4a464bd524e332477fad57c0875a5",
						k8sutils.NodeLabelGKEReservationBlocks:    "tpu7x-test-block-0001",
						k8sutils.NodeLabelGKEReservationSubBlocks: "tpu7x-test-block-0001-subblock-0002",
					},
				},
			},
			want: records.Attrs{
				NodePoolName:            "test-nodepool",
				TPUTopology:             "2x2x2",
				Spot:                    true,
				TPUAccelerator:          "tpu7x",
				TopologyBlockID:         "9a0e671424e45fd480ca172ad7a4e25d",
				TopologySubBlockID:      "6ce4a464bd524e332477fad57c0875a5",
				ReservationBlockName:    "tpu7x-test-block-0001",
				ReservationSubBlockName: "tpu7x-test-block-0001-subblock-0002",
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ExtractNodePoolAttrs(c.np)
			require.Equal(t, c.want, got)
		})
	}
}

func TestExtractSliceAttrs(t *testing.T) {
	cases := map[string]struct {
		s    *slicev1beta1.Slice
		want records.Attrs
	}{
		"empty": {
			s:    &slicev1beta1.Slice{},
			want: records.Attrs{},
		},
		"with topology block labels": {
			s: &slicev1beta1.Slice{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-slice",
					UID:  "slice-uid-123",
					Labels: map[string]string{
						k8sutils.LabelTPUProvisionerOwnerName:      "test-owner",
						k8sutils.LabelTPUProvisionerOwnerKind:      "JobSet",
						k8sutils.LabelTPUProvisionerOwnerNamespace: "default",
						k8sutils.NodeLabelGKETopologyBlock:         "9a0e671424e45fd480ca172ad7a4e25d",
						k8sutils.NodeLabelGKEReservationBlocks:     "tpu7x-test-block-0001",
					},
				},
				Spec: slicev1beta1.SliceSpec{
					Type:     "tpu-v5p",
					Topology: "2x2x2",
				},
			},
			want: records.Attrs{
				SliceName:            "test-slice",
				SliceUID:             "slice-uid-123",
				TPUAccelerator:       "tpu-v5p",
				TPUTopology:          "2x2x2",
				SliceOwnerName:       "test-owner",
				SliceOwnerKind:       "JobSet",
				SliceOwnerNamespace:  "default",
				TopologyBlockID:      "9a0e671424e45fd480ca172ad7a4e25d",
				ReservationBlockName: "tpu7x-test-block-0001",
				TPUChipCount:         8,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ExtractSliceAttrs(c.s)
			require.Equal(t, c.want, got)
		})
	}
}

func TestExtractLeaderWorkerSetAttrs(t *testing.T) {
	replicas := int32(2)
	cases := map[string]struct {
		lwsObj *lws.LeaderWorkerSet
		want   records.Attrs
	}{
		"empty": {
			lwsObj: &lws.LeaderWorkerSet{},
			want:   records.Attrs{},
		},
		"with topology block labels": {
			lwsObj: &lws.LeaderWorkerSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lws",
					Namespace: "test-ns",
					UID:       "lws-uid-123",
					Labels: map[string]string{
						k8sutils.NodeLabelGKETopologyBlock:     "9a0e671424e45fd480ca172ad7a4e25d",
						k8sutils.NodeLabelGKEReservationBlocks: "tpu7x-test-block-0001",
					},
				},
				Spec: lws.LeaderWorkerSetSpec{
					Replicas: &replicas,
					LeaderWorkerTemplate: lws.LeaderWorkerTemplate{
						WorkerTemplate: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								NodeSelector: map[string]string{
									k8sutils.NodeLabelGKETPUAccelerator: "tpu-v5p",
									k8sutils.NodeLabelGKETPUTopology:    "2x2x2",
									k8sutils.NodeLabelGKESpot:           "true",
								},
							},
						},
					},
				},
			},
			want: records.Attrs{
				LWSName:              "test-lws",
				LWSNamespace:         "test-ns",
				LWSUID:               "lws-uid-123",
				TPUAccelerator:       "tpu-v5p",
				TPUTopology:          "2x2x2",
				Spot:                 true,
				TopologyBlockID:      "9a0e671424e45fd480ca172ad7a4e25d",
				ReservationBlockName: "tpu7x-test-block-0001",
				TPUChipCount:         16, // 2 replicas * 8 chips = 16
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ExtractLeaderWorkerSetAttrs(c.lwsObj)
			require.Equal(t, c.want, got)
		})
	}
}

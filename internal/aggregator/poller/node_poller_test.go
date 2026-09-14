package poller

import (
	"context"
	"testing"

	slice "example.com/megamon/copied-slice-api/v1beta1"
	"example.com/megamon/internal/k8sutils"
	"github.com/stretchr/testify/require"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

type fakeGKEClient struct {
	nodePools []*containerv1beta1.NodePool
	err       error
}

func (f *fakeGKEClient) ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error) {
	return f.nodePools, f.err
}

func TestPollResources(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = jobset.AddToScheme(scheme)
	_ = slice.AddToScheme(scheme)

	tests := map[string]struct {
		status       string
		expectedDown bool
		expectedFail bool
	}{
		"status running": {
			status:       "RUNNING",
			expectedDown: false,
			expectedFail: false,
		},
		"status stopping": {
			status:       statusStopping,
			expectedDown: true,
			expectedFail: false,
		},
		"status deleting": {
			status:       statusDeleting,
			expectedDown: true,
			expectedFail: false,
		},
		"status error": {
			status:       statusError,
			expectedDown: false,
			expectedFail: true,
		},
		"status running with error": {
			status:       statusRunningWithError,
			expectedDown: false,
			expectedFail: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			fakeGKE := &fakeGKEClient{
				nodePools: []*containerv1beta1.NodePool{
					{
						Name:   "test-tpu-pool",
						Status: tc.status,
						PlacementPolicy: &containerv1beta1.PlacementPolicy{
							TpuTopology: "2x2x1",
						},
						Config: &containerv1beta1.NodeConfig{
							MachineType: "ct4p-hightpu-4t",
						},
					},
				},
			}

			provider := NewNodePoller(fakeClient, fakeGKE)
			nodePoolsUp, err := provider.PollResources(context.Background())

			require.NoError(t, err)
			require.NotNil(t, nodePoolsUp)
			require.Contains(t, nodePoolsUp, "test-tpu-pool")

			upRecord := nodePoolsUp["test-tpu-pool"]
			require.Equal(t, tc.expectedDown, upRecord.ExpectedDown, "ExpectedDown mismatch")
			require.Equal(t, tc.expectedFail, upRecord.Failed, "Failed mismatch")
			require.Equal(t, tc.status, upRecord.Status, "Status mismatch")
		})
	}
}

func TestPollResources_TopologyLabels(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = jobset.AddToScheme(scheme)
	_ = slice.AddToScheme(scheme)

	// NodePool without labels in Config.Labels (matching real GKE NodePool API objects)
	fakeGKE := &fakeGKEClient{
		nodePools: []*containerv1beta1.NodePool{
			{
				Name:   "test-tpu-pool",
				Status: "RUNNING",
				PlacementPolicy: &containerv1beta1.PlacementPolicy{
					TpuTopology: "2x2x1",
				},
				Config: &containerv1beta1.NodeConfig{
					MachineType: "tpu7x-standard-4t",
				},
			},
		},
	}

	// Node containing GCE topology block and reservation labels
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-0",
			Labels: map[string]string{
				k8sutils.NodeLabelGKENodepool:          "test-tpu-pool",
				k8sutils.NodeLabelGKETPUAccelerator:    "tpu7x",
				k8sutils.NodeLabelGKETPUTopology:       "2x2x1",
				k8sutils.NodeLabelGCETopologyBlock:     "9a0e671424e45fd480ca172ad7a4e25d",
				k8sutils.NodeLabelGCETopologySubBlock:  "6ce4a464bd524e332477fad57c0875a5",
				k8sutils.NodeLabelReservationBlocks:    "tpu7x-test-block-0001",
				k8sutils.NodeLabelReservationSubBlocks: "tpu7x-test-block-0001-subblock-0002",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&node).Build()
	provider := NewNodePoller(fakeClient, fakeGKE)
	nodePoolsUp, err := provider.PollResources(context.Background())

	require.NoError(t, err)
	require.NotNil(t, nodePoolsUp)
	require.Contains(t, nodePoolsUp, "test-tpu-pool")

	upRecord := nodePoolsUp["test-tpu-pool"]
	require.Equal(t, "9a0e671424e45fd480ca172ad7a4e25d", upRecord.Attrs.TopologyBlockID)
	require.Equal(t, "6ce4a464bd524e332477fad57c0875a5", upRecord.Attrs.TopologySubBlockID)
	require.Equal(t, "tpu7x-test-block-0001", upRecord.Attrs.ReservationBlockName)
	require.Equal(t, "tpu7x-test-block-0001-subblock-0002", upRecord.Attrs.ReservationSubBlockName)
	require.Equal(t, int32(1), upRecord.ReadyCount)
}

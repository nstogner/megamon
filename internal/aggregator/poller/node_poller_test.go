package poller

import (
	"context"
	"testing"

	slice "example.com/megamon/copied-slice-api/v1beta1"
	"github.com/stretchr/testify/require"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
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

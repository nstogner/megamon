package gkeclient

import (
	"context"

	"example.com/megamon/internal/k8sutils"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
)

type mockGKEClient struct {
	ClusterRef        string
	ContainersService *containerv1beta1.Service
	nodePools         []*containerv1beta1.NodePool
}

func (m *mockGKEClient) ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error) {
	return m.nodePools, nil
}

func createStubNodePool(nodePoolName, tpuAccelerator, tpuTopology string) *containerv1beta1.NodePool {
	return &containerv1beta1.NodePool{
		Name: nodePoolName,
		Config: &containerv1beta1.NodeConfig{
			MachineType: "tpu7x-standard-4t",
			DiskSizeGb:  100,
			ResourceLabels: map[string]string{
				k8sutils.NodePoolResourceLabelGKEAcceleratorType: tpuAccelerator,
			},
		},
		Autoscaling: &containerv1beta1.NodePoolAutoscaling{
			Enabled:      true,
			MinNodeCount: 1,
			MaxNodeCount: 3,
		},
		PlacementPolicy: &containerv1beta1.PlacementPolicy{
			TpuTopology: tpuTopology,
		},
	}
}

func CreateStubGKEClient() *mockGKEClient {
	return &mockGKEClient{
		nodePools: []*containerv1beta1.NodePool{
			createStubNodePool("a", "tpu7x", "4x4x4"),
			createStubNodePool("b", "tpu7x", "4x4x4"),
			createStubNodePool("c", "tpu7x", "4x4x4"),
		},
	}
}

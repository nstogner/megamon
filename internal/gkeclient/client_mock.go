package gkeclient

import (
	"context"
	"regexp"

	"example.com/megamon/internal/k8sutils"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockGKEClient struct {
	ClusterRef        string
	ContainersService *containerv1beta1.Service
	Client            client.Client
}

func (m *mockGKEClient) ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error) {
	// build fake GKE node pool object based on k8s nodes present, assumes a node naming convention
	var nodeList corev1.NodeList
	if err := m.Client.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	nodePoolMap := make(map[string]int64)
	nodePoolAttrs := make(map[string]struct {
		Accelerator string
		Topology    string
	})

	// Regex to match [nodepool_name]-n-n-n
	re := regexp.MustCompile(`^(.*)-\d+-\d+-\d+(?:-\d+)*$`)

	for _, node := range nodeList.Items {
		matches := re.FindStringSubmatch(node.Name)
		if len(matches) < 2 {
			continue
		}
		nodePoolName := matches[1]
		nodePoolMap[nodePoolName]++

		if _, ok := nodePoolAttrs[nodePoolName]; !ok {
			nodePoolAttrs[nodePoolName] = struct {
				Accelerator string
				Topology    string
			}{
				Accelerator: node.Labels[k8sutils.NodeLabelGKETPUAccelerator],
				Topology:    node.Labels[k8sutils.NodeLabelGKETPUTopology],
			}
		}
	}

	var nodePools []*containerv1beta1.NodePool
	for name, count := range nodePoolMap {
		attrs := nodePoolAttrs[name]
		nodePools = append(nodePools, createStubNodePool(name, attrs.Accelerator, attrs.Topology, count))
	}

	return nodePools, nil
}

func createStubNodePool(nodePoolName, tpuAccelerator, tpuTopology string, nodeCount int64) *containerv1beta1.NodePool {
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
			MinNodeCount: nodeCount,
			MaxNodeCount: nodeCount,
		},
		PlacementPolicy: &containerv1beta1.PlacementPolicy{
			TpuTopology: tpuTopology,
		},
	}
}

func CreateStubGKEClient(c client.Client) *mockGKEClient {
	return &mockGKEClient{
		Client: c,
	}
}

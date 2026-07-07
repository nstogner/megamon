// Package poller is responsible for discovering Kubernetes resources and calculating their current, raw operational status.
// It acts as the "Reader" of the cluster state for the Aggregator, without any knowledge of historical events or storage.
package poller

import (
	"context"
	"fmt"

	"example.com/megamon/internal/aggregator/utils"
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("upness")

const (
	statusStopping         = "STOPPING"
	statusDeleting         = "DELETING"
	statusError            = "ERROR"
	statusRunningWithError = "RUNNING_WITH_ERROR"
)

type GKEClient interface {
	ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error)
}

type NodePoller struct {
	Client client.Client
	GKE    GKEClient
}

func NewNodePoller(c client.Client, gke GKEClient) *NodePoller {
	return &NodePoller{
		Client: c,
		GKE:    gke,
	}
}

func (p *NodePoller) PollResources(ctx context.Context) (map[string]records.Upness, error) {
	nodePoolsUp := make(map[string]records.Upness)
	var nodeList corev1.NodeList
	if err := p.Client.List(ctx, &nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	npList, err := p.GKE.ListNodePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing node pools: %w", err)
	}
	for _, np := range npList {
		func() {
			if !utils.IsTPUNodePool(np) {
				return
			}
			up := records.Upness{
				Attrs:        utils.ExtractNodePoolAttrs(np),
				Status:       np.Status,
				ExpectedDown: np.Status == statusStopping || np.Status == statusDeleting,
				Failed:       np.Status == statusError || np.Status == statusRunningWithError,
			}
			expectedCount, err := utils.GetExpectedTPUNodePoolSize(np)
			if err != nil {
				log.Error(err, "failed to get expected TPU node pool size", "nodepool", np.Name)
				return
			}
			up.ExpectedCount = expectedCount
			if tpuChipCount, err := k8sutils.GetTpuTopologyToChipCount(up.TPUTopology); err != nil {
				log.Error(err, "failed to convert TPU topology to chip count", "nodepool", np.Name)
			} else {
				// #nosec G115
				up.TPUChipCount = int32(tpuChipCount)
			}
			nodePoolsUp[np.Name] = up
		}()
	}

	for _, node := range nodeList.Items {
		nodeStatus := k8sutils.IsNodeReady(&node)

		// Node pool mapping:
		if npName, ok := k8sutils.GetNodePool(&node); ok {
			func() {
				if !k8sutils.IsTPUNode(&node) {
					return
				}
				up, ok := nodePoolsUp[npName]
				if !ok {
					log.Info("WARNING: found Node for node pool that was not parsed", "node", node.Name, "nodepool", npName)
					return
				}
				if up.ExpectedCount == 0 {
					var err error
					up.ExpectedCount, err = k8sutils.GetExpectedTPUNodePoolSize(&node)
					if err != nil {
						log.Error(err, "failed to get expected TPU node pool size", "node", node.Name)
						return
					}
				}

				if nodeStatus == corev1.ConditionTrue {
					up.ReadyCount++
				} else if nodeStatus == corev1.ConditionUnknown {
					up.UnknownCount++
				}
				nodePoolsUp[npName] = up
			}()
		}
	}

	log.V(3).Info("DEBUG", "nodePoolsUp", nodePoolsUp)

	return nodePoolsUp, nil
}

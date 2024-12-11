package gkeclient

import (
	"context"
	"fmt"

	containerv1beta1 "google.golang.org/api/container/v1beta1"
)

type Client struct {
	// ClusterRef should be of the format `projects/*/locations/*/clusters/*`
	ClusterRef        string
	ContainersService *containerv1beta1.Service
}

func (c *Client) ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error) {
	npListResp, err := c.ContainersService.Projects.Locations.Clusters.NodePools.List(c.ClusterRef).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("listing node pools: %w", err)
	}
	return npListResp.NodePools, nil
}

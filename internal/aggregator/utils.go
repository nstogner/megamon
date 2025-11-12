package aggregator

import (
	"fmt"
	"strconv"
	"strings"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func extractJobSetAttrs(js *jobset.JobSet) records.Attrs {
	var attrs records.Attrs
	var chipCount int32
	for _, rj := range js.Spec.ReplicatedJobs {
		// Example:
		//
		// nodeSelector:
		//   cloud.google.com/gke-tpu-accelerator: tpu-v5p-slice
		//   cloud.google.com/gke-tpu-topology: 2x2x1
		//   cloud.google.com/gke-spot: "true"
		//
		for key, val := range rj.Template.Spec.Template.Spec.NodeSelector {
			switch key {
			case k8sutils.NodeLabelGKETPUAccelerator:
				attrs.TPUAccelerator = val
			case k8sutils.NodeLabelGKETPUTopology:
				attrs.TPUTopology = val
				if topologyChipCount, err := k8sutils.GetTpuTopologyToChipCount(val); err == nil {
					chipCount += rj.Replicas * int32(topologyChipCount)
				} else {
					log.Error(err, "error converting TPU topology to chip count", "topology", val)
				}
			case k8sutils.NodeLabelGKESpot:
				attrs.Spot = val == "true"
			}
		}
	}

	attrs.JobSetName = js.Name
	attrs.JobSetNamespace = js.Namespace
	attrs.JobSetUID = string(js.UID)
	attrs.TPUChipCount = chipCount

	return attrs
}

func extractNodeAttrs(node *corev1.Node) records.Attrs {
	var attrs records.Attrs

	if node.Labels != nil {
		if val, ok := node.Labels[k8sutils.NodeLabelGKETPUAccelerator]; ok {
			attrs.TPUAccelerator = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKETPUTopology]; ok {
			attrs.TPUTopology = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKESpot]; ok {
			attrs.Spot = val == "true"
		}
	}

	return attrs
}

func isTPUNodePool(np *containerv1beta1.NodePool) bool {
	return np.PlacementPolicy != nil && np.PlacementPolicy.TpuTopology != ""
}

func getExpectedTPUNodePoolSize(np *containerv1beta1.NodePool) (int32, error) {
	if np.PlacementPolicy == nil {
		return 0, fmt.Errorf("no placement policy")
	}

	topoVal := np.PlacementPolicy.TpuTopology
	if topoVal == "" {
		return 0, fmt.Errorf("no topology")
	}

	acceleratorCount, err := machineTypeToChipCount(np.Config.MachineType)
	if err != nil {
		return 0, fmt.Errorf("failed to convert machine type to chip count: %w", err)
	}

	split := strings.Split(topoVal, "x")
	if len(split) < 2 {
		return 0, fmt.Errorf("invalid topology: %q", topoVal)
	}
	product := 1
	for _, s := range split {
		x, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("invalid topology: %q, could not convert %q to int: %w", topoVal, s, err)
		}
		product *= x
	}

	return int32(product / acceleratorCount), nil
}

func machineTypeToChipCount(mt string) (int, error) {
	// Example: "ct5p-hightpu-4t"

	split := strings.Split(mt, "-")
	if len(split) < 2 {
		return 0, fmt.Errorf("unable to parse tpu machine type: %q", mt)
	}
	acceleratorCountVal := strings.TrimSuffix(split[len(split)-1], "t")

	acceleratorCount, err := strconv.Atoi(acceleratorCountVal)
	if err != nil {
		return 0, fmt.Errorf("failed to parse accelerator count: %w", err)
	}
	if acceleratorCount < 1 {
		return 0, fmt.Errorf("invalid accelerator count: %d", acceleratorCount)
	}

	return acceleratorCount, nil
}

func extractNodePoolAttrs(np *containerv1beta1.NodePool) records.Attrs {
	var attrs records.Attrs

	attrs.NodePoolName = np.Name
	if np.PlacementPolicy != nil {
		attrs.TPUTopology = np.PlacementPolicy.TpuTopology
	}
	if np.Config != nil {
		attrs.Spot = np.Config.Spot
		if np.Config.ResourceLabels != nil {
			// Check for the goog-gke-accelerator-type label
			if v, ok := np.Config.ResourceLabels[k8sutils.NodePoolResourceLabelGKEAcceleratorType]; ok && v != "" {
				attrs.TPUAccelerator = v
			}
		}
	}
	return attrs
}

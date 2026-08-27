package utils

import (
	"fmt"
	"strconv"
	"strings"

	slicev1beta1 "example.com/megamon/copied-slice-api/v1beta1"
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

var log = logf.Log.WithName("aggregator-utils")

func ExtractJobSetAttrs(js *jobset.JobSet) records.Attrs {
	var attrs records.Attrs
	var chipCount int32
	for _, rj := range js.Spec.ReplicatedJobs {
		for key, val := range rj.Template.Spec.Template.Spec.NodeSelector {
			switch key {
			case k8sutils.NodeLabelGKETPUAccelerator:
				attrs.TPUAccelerator = val
			case k8sutils.NodeLabelGKETPUTopology:
				attrs.TPUTopology = val
				if topologyChipCount, err := k8sutils.GetTpuTopologyToChipCount(val); err == nil {
					// #nosec G115
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

	if js.Labels != nil {
		if val, ok := js.Labels[k8sutils.NodeLabelGKETopologyBlock]; ok {
			attrs.TopologyBlockID = val
		}
		if val, ok := js.Labels[k8sutils.NodeLabelGKEReservationBlocks]; ok {
			attrs.ReservationBlockName = val
		}
	}

	return attrs
}

func ExtractLeaderWorkerSetAttrs(lwsObj *lws.LeaderWorkerSet) records.Attrs {
	var attrs records.Attrs
	var chipCount int32

	if lwsObj.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec.NodeSelector != nil {
		for key, val := range lwsObj.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec.NodeSelector {
			switch key {
			case k8sutils.NodeLabelGKETPUAccelerator:
				attrs.TPUAccelerator = val
			case k8sutils.NodeLabelGKETPUTopology:
				attrs.TPUTopology = val
				if topologyChipCount, err := k8sutils.GetTpuTopologyToChipCount(val); err == nil {
					replicas := int32(1)
					if lwsObj.Spec.Replicas != nil {
						replicas = *lwsObj.Spec.Replicas
					}
					// #nosec G115
					chipCount = replicas * int32(topologyChipCount)
				}
			case k8sutils.NodeLabelGKESpot:
				attrs.Spot = val == "true"
			}
		}
	}

	attrs.LWSName = lwsObj.Name
	attrs.LWSNamespace = lwsObj.Namespace
	attrs.LWSUID = string(lwsObj.UID)
	attrs.TPUChipCount = chipCount

	if lwsObj.Labels != nil {
		if val, ok := lwsObj.Labels[k8sutils.NodeLabelGKETopologyBlock]; ok {
			attrs.TopologyBlockID = val
		}
		if val, ok := lwsObj.Labels[k8sutils.NodeLabelGKEReservationBlocks]; ok {
			attrs.ReservationBlockName = val
		}
	}

	return attrs
}

func ExtractNodeAttrs(node *corev1.Node) records.Attrs {
	var attrs records.Attrs

	if node.Labels != nil {
		if val, ok := node.Labels[k8sutils.NodeLabelGKETPUAccelerator]; ok {
			attrs.TPUAccelerator = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKETPUTopology]; ok {
			attrs.TPUTopology = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKETPUSlice]; ok {
			attrs.SliceName = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKESpot]; ok {
			attrs.Spot = val == "true"
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKETopologyBlock]; ok {
			attrs.TopologyBlockID = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKETopologySubBlock]; ok {
			attrs.TopologySubBlockID = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKEReservationBlocks]; ok {
			attrs.ReservationBlockName = val
		}
		if val, ok := node.Labels[k8sutils.NodeLabelGKEReservationSubBlocks]; ok {
			attrs.ReservationSubBlockName = val
		}
	}

	return attrs
}

func IsTPUNodePool(np *containerv1beta1.NodePool) bool {
	return np.PlacementPolicy != nil && np.PlacementPolicy.TpuTopology != ""
}

func GetExpectedTPUNodePoolSize(np *containerv1beta1.NodePool) (int32, error) {
	if np.PlacementPolicy == nil {
		return 0, fmt.Errorf("no placement policy")
	}

	topoVal := np.PlacementPolicy.TpuTopology
	if topoVal == "" {
		return 0, fmt.Errorf("no topology")
	}

	acceleratorCount, err := MachineTypeToChipCount(np.Config.MachineType)
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

	// #nosec G115
	return int32(product / acceleratorCount), nil
}

func MachineTypeToChipCount(mt string) (int, error) {
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

func ExtractNodePoolAttrs(np *containerv1beta1.NodePool) records.Attrs {
	var attrs records.Attrs

	attrs.NodePoolName = np.Name
	if np.PlacementPolicy != nil {
		attrs.TPUTopology = np.PlacementPolicy.TpuTopology
	}
	if np.Config != nil {
		attrs.Spot = np.Config.Spot
		if np.Config.ResourceLabels != nil {
			if v, ok := np.Config.ResourceLabels[k8sutils.NodePoolResourceLabelGKEAcceleratorType]; ok && v != "" {
				attrs.TPUAccelerator = v
			}
		}
		if np.Config.Labels != nil {
			if v, ok := np.Config.Labels[k8sutils.NodeLabelGKETopologyBlock]; ok {
				attrs.TopologyBlockID = v
			}
			if v, ok := np.Config.Labels[k8sutils.NodeLabelGKETopologySubBlock]; ok {
				attrs.TopologySubBlockID = v
			}
			if v, ok := np.Config.Labels[k8sutils.NodeLabelGKEReservationBlocks]; ok {
				attrs.ReservationBlockName = v
			}
			if v, ok := np.Config.Labels[k8sutils.NodeLabelGKEReservationSubBlocks]; ok {
				attrs.ReservationSubBlockName = v
			}
		}
	}
	return attrs
}

func ExtractSliceAttrs(s *slicev1beta1.Slice) records.Attrs {
	attrs := records.Attrs{
		SliceName:      s.Name,
		SliceUID:       string(s.UID),
		TPUAccelerator: string(s.Spec.Type),
		TPUTopology:    s.Spec.Topology,
	}

	if s.Labels != nil {
		if val, ok := s.Labels[k8sutils.LabelTPUProvisionerOwnerName]; ok {
			attrs.SliceOwnerName = val
		}
		if val, ok := s.Labels[k8sutils.LabelTPUProvisionerOwnerKind]; ok {
			attrs.SliceOwnerKind = val
		}
		if val, ok := s.Labels[k8sutils.LabelTPUProvisionerOwnerNamespace]; ok {
			attrs.SliceOwnerNamespace = val
		}
		if val, ok := s.Labels[k8sutils.NodeLabelGKETopologyBlock]; ok {
			attrs.TopologyBlockID = val
		}
		if val, ok := s.Labels[k8sutils.NodeLabelGKEReservationBlocks]; ok {
			attrs.ReservationBlockName = val
		}
	}

	if chipCount, err := k8sutils.GetTpuTopologyToChipCount(s.Spec.Topology); err == nil {
		// #nosec G115
		attrs.TPUChipCount = int32(chipCount)
	}

	return attrs
}

package k8sutils

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func GetNodePool(node *corev1.Node) (string, bool) {
	if node.Labels == nil {
		return "", false
	}
	val, ok := node.Labels[NodeLabelGKENodepool]
	return val, ok
}

func IsTPUNode(node *corev1.Node) bool {
	if node.Labels == nil {
		return false
	}
	_, ok := node.Labels[NodeLabelGKETPUTopology]
	return ok
}

func IsJobSetActive(js *jobset.JobSet) bool {
	for _, c := range js.Status.Conditions {
		if c.Status == metav1.ConditionTrue {
			switch jobset.JobSetConditionType(c.Type) {
			case jobset.JobSetFailed, jobset.JobSetCompleted, jobset.JobSetSuspended:
				return false
			}
		}
	}
	return true
}

func GetJobSetReplicas(js *jobset.JobSet) (int32, int32) {
	var specifiedReplicas int32
	var readyReplicas int32

	for _, rj := range js.Spec.ReplicatedJobs {
		specifiedReplicas += rj.Replicas
	}

	for _, rjs := range js.Status.ReplicatedJobsStatus {
		readyReplicas += rjs.Ready
	}

	return specifiedReplicas, readyReplicas
}

func GetJobSetForNode(node *corev1.Node) (string, string) {
	if node.Labels == nil {
		return "", ""
	}

	// TODO: Add additional label checks for different provisioning
	// paths.
	// OR:
	// Consider deprecating these metrics in favor of nodepool-based
	// metrics with joins to JobSets.

	jsNS := node.Labels[NodeLabelTPUProvisionerJobSetNamespace]
	jsName := node.Labels[NodeLabelTPUProvisionerJobSetName]
	return jsNS, jsName
}

func GetExpectedNodeCount(js *jobset.JobSet) int32 {
	var count int32

	for _, rj := range js.Spec.ReplicatedJobs {
		parallelism := int32(1)
		if rj.Template.Spec.Parallelism != nil {
			parallelism = *rj.Template.Spec.Parallelism
		}
		count += rj.Replicas * parallelism
	}

	return count
}

func IsNodeReady(node *corev1.Node, unknownThreshold float64) corev1.ConditionStatus {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			switch c.Status {
			case corev1.ConditionTrue:
				return corev1.ConditionTrue
			case corev1.ConditionFalse:
				return corev1.ConditionFalse
			case corev1.ConditionUnknown:
				// At large scale a given Node may be in an unknown state for a short period of time.
				// However the workloads running on that Node could still be functioning.
				if time.Since(c.LastTransitionTime.Time) < 3*time.Minute {
					// special case, if unknownThreshold == 1.0
					if unknownThreshold == 1.0 {
						return corev1.ConditionTrue
					} else {
						return corev1.ConditionUnknown
					}
				}
			default:
				return corev1.ConditionUnknown
			}
		}
	}
	return corev1.ConditionUnknown
}

func GetExpectedTPUNodePoolSize(node *corev1.Node) (int32, error) {
	if node.Labels == nil {
		return 0, fmt.Errorf("no annotations")
	}
	const topoKey = NodeLabelGKETPUTopology
	topoVal, ok := node.Labels[topoKey]
	if !ok {
		return 0, fmt.Errorf("no topology annotation: %q", topoKey)
	}
	const acceleratorCountKey = NodeLabelGKEAcceleratorCount
	acceleratorCountVal, ok := node.Labels[acceleratorCountKey]
	if !ok {
		return 0, fmt.Errorf("no accelerator annotation: %q", acceleratorCountKey)
	}
	acceleratorCount, err := strconv.Atoi(acceleratorCountVal)
	if err != nil {
		return 0, fmt.Errorf("failed to parse accelerator count: %w", err)
	}
	if acceleratorCount < 1 {
		return 0, fmt.Errorf("invalid accelerator count: %d", acceleratorCount)
	}

	product, err := GetTpuTopologyToChipCount(topoVal)
	if err != nil {
		return 0, err
	}
	return int32(product / acceleratorCount), nil
}

func GetTpuTopologyToChipCount(topo string) (int, error) {
	// TODO: Do we need to validate expectedDims? GKE won't run the jobset if this is invalid?
	split := strings.Split(topo, "x")
	if len(split) < 2 {
		return 0, fmt.Errorf("invalid topology: %q", topo)
	}
	product := 1
	for _, s := range split {
		x, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("invalid topology: %v, could not convert %q to int: %w", topo, s, err)
		}
		product *= x
	}
	return product, nil
}

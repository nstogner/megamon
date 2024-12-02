package aggregator

import (
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func extractJobSetAttrs(js *jobset.JobSet) records.Attrs {
	var attrs records.Attrs

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
			case k8sutils.NodeLabelGKESpot:
				attrs.Spot = val == "true"
			}
		}
	}

	attrs.JobSetName = js.Name
	attrs.JobSetNamespace = js.Namespace
	attrs.JobSetUID = string(js.UID)

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

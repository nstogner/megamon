package aggregator

import (
	"example.com/megamon/internal/records"
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
			case "cloud.google.com/gke-tpu-accelerator":
				attrs.TPUAccelerator = val
			case "cloud.google.com/gke-tpu-topology":
				attrs.TPUTopology = val
			case "cloud.google.com/gke-spot":
				attrs.Spot = val == "true"
			}
		}
	}

	attrs.JobSetName = js.Name
	attrs.JobSetNamespace = js.Namespace

	return attrs
}

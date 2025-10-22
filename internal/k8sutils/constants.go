package k8sutils

const (
	NodeLabelGKETPUAccelerator   = "cloud.google.com/gke-tpu-accelerator"
	NodeLabelGKEAcceleratorCount = "cloud.google.com/gke-accelerator-count"
	NodeLabelGKETPUTopology      = "cloud.google.com/gke-tpu-topology"
	NodeLabelGKESpot             = "cloud.google.com/gke-spot"
	NodeLabelGKENodepool         = "cloud.google.com/gke-nodepool"

	NodeLabelTPUProvisionerJobSetNamespace = "google.com/tpu-provisioner-jobset-namespace"
	NodeLabelTPUProvisionerJobSetName      = "google.com/tpu-provisioner-jobset-name"

	NodePoolLabelGKEAcceleratorType = "goog-gke-accelerator-type"

	PodLabelJobName    = "batch.kubernetes.io/job-name"
	PodLabelJobSetName = "jobset.sigs.k8s.io/jobset-name"
)

package k8sutils

import (
	"encoding/json"

	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

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

func IsJobSetUp(js *jobset.JobSet) bool {
	var specifiedReplicas int32
	var readyReplicas int32

	for _, rj := range js.Spec.ReplicatedJobs {
		specifiedReplicas += rj.Replicas
	}

	for _, rjs := range js.Status.ReplicatedJobsStatus {
		readyReplicas += rjs.Ready
	}

	return specifiedReplicas == readyReplicas
}

func GetJobSetForNode(node *corev1.Node) (string, bool) {
	if node.Labels == nil {
		return "", false
	}

	jsName, ok := node.Labels["google.com/tpu-provisioner-jobset-name"]
	return jsName, ok
}

func GetExpectedNodeCount(js *jobset.JobSet) int {
	var count int

	for _, rj := range js.Spec.ReplicatedJobs {
		parallelism := int32(1)
		if rj.Template.Spec.Parallelism != nil {
			parallelism = *rj.Template.Spec.Parallelism
		}
		count += int(rj.Replicas * parallelism)
	}

	return count
}

func IsNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func GetJobsetRecords(js *jobset.JobSet) (records.EventRecords, error) {
	var rec records.EventRecords
	if js.GetAnnotations() == nil {
		return rec, nil
	}
	val, ok := js.Annotations[records.JobSetRecordsAnnotationKey]
	if !ok {
		return rec, nil
	}
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func SetJobsetRecords(js *jobset.JobSet, rec records.EventRecords) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if js.Annotations == nil {
		js.Annotations = map[string]string{}
	}
	js.Annotations[records.JobSetRecordsAnnotationKey] = string(data)
	return nil
}

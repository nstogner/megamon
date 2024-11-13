package k8sutils

import (
	"encoding/json"

	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func GetNodePool(node *corev1.Node) (string, bool) {
	if node.Labels == nil {
		return "", false
	}
	val, ok := node.Labels["cloud.google.com/gke-nodepool"]
	return val, ok
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

	jsNS := node.Labels["google.com/tpu-provisioner-jobset-namespace"]
	jsName := node.Labels["google.com/tpu-provisioner-jobset-name"]
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

func IsNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func GetEventRecordsFromConfigMap(cm *corev1.ConfigMap) (map[string]records.EventRecords, error) {
	recs := make(map[string]records.EventRecords)
	if cm.Data == nil {
		return recs, nil
	}
	for k, v := range cm.Data {
		var rec records.EventRecords
		if err := json.Unmarshal([]byte(v), &rec); err != nil {
			return recs, err
		}
		recs[k] = rec
	}
	return recs, nil
}

func SetEventRecordsInConfigMap(cm *corev1.ConfigMap, recs map[string]records.EventRecords) error {
	cm.Data = make(map[string]string)
	for k, rec := range recs {
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[k] = string(data)
	}
	return nil
}

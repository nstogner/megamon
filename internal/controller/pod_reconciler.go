package controller

import (
	"context"

	"example.com/megamon/internal/aggregator"
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	jobScheduledNodePoolLabel = "megamon.tbd/scheduled-node-pool"
)

// PodReconciler watches for JobSet Job leader Pods and labels the Job once Node scheduling
// has occurred to keep track of dynamic Job-to-NodePool relationships.
type PodReconciler struct {
	client.Client
	Aggregator                  *aggregator.Aggregator
	Scheme                      *runtime.Scheme
	DisableNodePoolJobLabelling bool
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//log := log.FromContext(ctx)
	//log.Info("reconciling", "req", req.NamespacedName)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Example labels:
	//   batch.kubernetes.io/job-name: example-single-3-rs-s-0
	//   controller-uid: dd51ca12-4d86-4229-88bf-b0996bd74c70
	//   job-name: example-single-3-rs-s-0
	//   jobset.sigs.k8s.io/job-index: "0"
	//   jobset.sigs.k8s.io/job-key: b70f6cf4dd38e2c17d612d8f2393cc6a5bc67841
	//   jobset.sigs.k8s.io/jobset-name: example-single-3

	var rec records.ScheduledJob
	if pod.Labels == nil {
		return ctrl.Result{}, nil
	}
	if jobName, ok := pod.Labels["batch.kubernetes.io/job-name"]; ok {
		rec.JobName = jobName
	} else {
		return ctrl.Result{}, nil
	}
	if jobSetName, ok := pod.Labels["jobset.sigs.k8s.io/jobset-name"]; ok {
		rec.JobSetName = jobSetName
	} else {
		return ctrl.Result{}, nil
	}

	var node corev1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	nodePool, ok := k8sutils.GetNodePool(&node)
	if !ok {
		return ctrl.Result{}, nil
	}

	r.Aggregator.SetNodePoolScheduling(nodePool, rec)

	if !r.DisableNodePoolJobLabelling {
		var jobRef metav1.OwnerReference
		for _, ref := range pod.OwnerReferences {
			if ref.Kind == "Job" {
				jobRef = ref
				break
			}
		}
		if jobRef.Name == "" {
			return ctrl.Result{}, nil
		}

		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: jobRef.Name}, &job); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if job.Labels == nil {
			job.Labels = make(map[string]string)
		}
		job.Labels[jobScheduledNodePoolLabel] = nodePool
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

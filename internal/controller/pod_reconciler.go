package controller

import (
	"context"

	"example.com/megamon/internal/k8sutils"
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
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//log := log.FromContext(ctx)
	//log.Info("reconciling", "req", req.NamespacedName)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var node corev1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	nodePool, ok := k8sutils.GetNodePool(&node)
	if !ok {
		return ctrl.Result{}, nil
	}

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

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

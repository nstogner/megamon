package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// JobSetReconciler reconciles a Guestbook object
type JobSetReconciler struct {
	Disabled bool

	//Recorder *eventrecorder.Recorder

	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get

func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if r.Disabled {
		return ctrl.Result{}, nil
	}

	/*
		var js jobset.JobSet
		if err := r.Get(ctx, req.NamespacedName, &js); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		if err := r.Recorder.Record(ctx, &js); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to record jobset events: %w", err)
		}
	*/

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JobSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jobset.JobSet{}).
		Named("jobset").
		Complete(r)
}

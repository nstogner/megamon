package controller

import (
	"context"

	slice "example.com/megamon/slice-api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SliceReconciler reconciles a slice object
type SliceReconciler struct {
	Name string
	client.Client
	Disabled bool
	Scheme   *runtime.Scheme
}

// TODO: Is this correct?
// +kubebuilder:rbac:groups=accelerator.gke.io,resources=slices,verbs=get;list;watch
// +kubebuilder:rbac:groups=accelerator.gke.io,resources=slices/status,verbs=get

func (r *SliceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	rlog := log.FromContext(ctx)
	rlog.V(3).Info("Reconciling Slice", "name", req.Name)

	if r.Disabled {
		return ctrl.Result{}, nil
	}
	var slice slice.Slice
	if err := r.Get(ctx, req.NamespacedName, &slice); err != nil {
		rlog.Error(err, "Error getting slice", "name", slice.Name)
	}
	rlog.V(5).Info("Debug slice", "slice", slice)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SliceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "slice"
	if r.Name != "" {
		name = r.Name
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&slice.Slice{}).
		Named(name).
		Complete(r)
}

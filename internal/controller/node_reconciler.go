package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// JobSetReconciler reconciles a Guestbook object
type NodeReconciler struct {
	NodePoolEventsConfigMapRef types.NamespacedName

	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;update;patch

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//log := log.FromContext(ctx)
	//log.Info("reconciling", "req", req.NamespacedName)

	// TODO: implemented logic to reconcile node if needed.

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		//For(&corev1.ConfigMap{}).
		//Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(handler.MapFunc(r.mapNodeToNodePool))).
		//Named("node").
		Complete(r)
}

func (r *NodeReconciler) mapNodeToNodePool(ctx context.Context, obj client.Object) []reconcile.Request {
	node := obj.(*corev1.Node)
	if node.Labels == nil {
		return nil
	}
	gkeNodepool, ok := node.Labels["cloud.google.com/gke-nodepool"]
	if !ok {
		return nil
	}

	ref := r.NodePoolEventsConfigMapRef
	ref.Name = ref.Name + "." + gkeNodepool
	return []reconcile.Request{{NamespacedName: ref}}
}

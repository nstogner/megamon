package controller

import (
	"context"
	"fmt"
	"time"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// JobSetReconciler reconciles a Guestbook object
type JobSetReconciler struct {
	Disabled bool

	JobSetEventsConfigMapRef types.NamespacedName

	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get

func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.Disabled {
		return ctrl.Result{}, nil
	}

	log := log.FromContext(ctx)

	var js jobset.JobSet
	if err := r.Get(ctx, req.NamespacedName, &js); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cm corev1.ConfigMap
	if err := r.Get(ctx, r.JobSetEventsConfigMapRef, &cm); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get event records configmap: %w", err)
	}
	rec, err := k8sutils.GetEventRecordsFromConfigMap(&cm, k8sutils.JobSetEventsKey(&js))
	if err != nil {
		log.Error(err, "failed to get event records from configmap", "jobset", js.Name, "configmap", cm.Name)
		return ctrl.Result{}, nil
	}

	var changed bool
	isUp := k8sutils.IsJobSetUp(&js)
	if len(rec.UpEvents) == 0 {
		rec.UpEvents = append(rec.UpEvents, records.UpEvent{
			Up:        isUp,
			Timestamp: time.Now(),
		})
		changed = true
	} else {
		last := rec.UpEvents[len(rec.UpEvents)-1]
		if last.Up != isUp {
			rec.UpEvents = append(rec.UpEvents, records.UpEvent{
				Up:        isUp,
				Timestamp: time.Now(),
			})
			changed = true
		}
	}

	if changed {
		if err := k8sutils.SetEventRecordsInConfigMap(&cm, k8sutils.JobSetEventsKey(&js), rec); err != nil {
			log.Error(err, "failed to set jobset records in configmap")
			return ctrl.Result{}, nil
		}
		if err := r.Update(ctx, &cm); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update events configmap: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JobSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jobset.JobSet{}).
		Named("jobset").
		Complete(r)
}

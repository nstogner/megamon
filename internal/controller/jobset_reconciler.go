package controller

import (
	"context"
	"fmt"
	"time"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// JobSetReconciler reconciles a Guestbook object
type JobSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get;update;patch

func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var js jobset.JobSet
	if err := r.Get(ctx, req.NamespacedName, &js); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rec, err := k8sutils.GetJobsetRecords(&js)
	if err != nil {
		log.Error(err, "failed to get jobset records", "jobset", js.Name)
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
		k8sutils.SetJobsetRecords(&js, rec)
		if err := r.Update(ctx, &js); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update jobset records: %w", err)
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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"

	slicev1beta1 "example.com/megamon/copied-slice-api/v1beta1"
	"example.com/megamon/internal/aggregator"
	"example.com/megamon/internal/aggregator/utils"
	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
)

type WorkloadReconciler struct {
	client.Client
	Name                   string
	Scheme                 *runtime.Scheme
	EventLog               aggregator.EventLog
	LeaderWorkerSetEnabled bool
	SliceEnabled           bool
	UnknownCountThreshold  float64

	SliceOwnerMapConfigMapRef types.NamespacedName
}

func (r *WorkloadReconciler) Init() {
	if r.Name == "" {
		r.Name = "workload-reconciler"
	}
}

const sliceOwnerMapKey = "slice-owner-map.json"

// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// NB: RBAC for slice resources are added via kustomize, do not add kubebuilder annotations here

func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("workload-reconciler")
	log.V(3).Info("reconciling workloads")

	var sliceOwnerMap map[string]records.OwnerInfo
	if r.SliceEnabled {
		sliceOwnerMap = make(map[string]records.OwnerInfo)
		var cm corev1.ConfigMap
		if err := r.Get(ctx, r.SliceOwnerMapConfigMapRef, &cm); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("failed to get slice owner map ConfigMap: %w", err)
			}
		} else if data, ok := cm.Data[sliceOwnerMapKey]; ok {
			if err := json.Unmarshal([]byte(data), &sliceOwnerMap); err != nil {
				log.Error(err, "failed to unmarshal slice owner map, starting fresh")
			}
		}
	}

	now := time.Now()

	// 1. List all resources
	var jobsetList jobset.JobSetList
	var lwsList lws.LeaderWorkerSetList
	var sliceList slicev1beta1.SliceList
	var nodeList corev1.NodeList

	if err := r.List(ctx, &jobsetList); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing jobsets: %w", err)
	}

	if r.LeaderWorkerSetEnabled {
		if err := r.List(ctx, &lwsList); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing leaderworkersets: %w", err)
		}
	}

	if r.SliceEnabled {
		if err := r.List(ctx, &sliceList); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing slices: %w", err)
		}
	}

	if !r.SliceEnabled {
		if err := r.List(ctx, &nodeList); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing nodes: %w", err)
		}
	}

	// 2. Compute Upness and update maps
	jobsetsUp, jobsetNodesUp := r.processJobSets(jobsetList, nodeList)

	lwsUp := make(map[string]records.Upness)
	if r.LeaderWorkerSetEnabled {
		lwsUp = r.processLeaderWorkerSets(lwsList)
	}

	slicesUp := make(map[string]records.Upness)
	if r.SliceEnabled {
		slicesUp = r.processSlices(ctx, sliceList, sliceOwnerMap)
	}

	// 3. Save to Event Log in a single batch
	changes := map[string]map[string]records.Upness{
		records.EventKeyJobSets: jobsetsUp,
	}

	if !r.SliceEnabled {
		changes[records.EventKeyJobSetNodes] = jobsetNodesUp
	}
	if r.LeaderWorkerSetEnabled {
		changes[records.EventKeyLeaderWorkerSets] = lwsUp
	}
	if r.SliceEnabled {
		changes[records.EventKeySlices] = slicesUp
	}

	if err := r.EventLog.AppendStateChanges(ctx, now, changes); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to append state changes: %w", err)
	}

	return ctrl.Result{}, nil
}

// processJobSets extracts upness status for JobSets and their underlying Nodes.
// It evaluates JobSet replicas (expected vs ready) and, if slicing is disabled,
// it also calculates node-level upness by mapping cluster nodes back to their owning JobSet.
func (r *WorkloadReconciler) processJobSets(jobsetList jobset.JobSetList, nodeList corev1.NodeList) (map[string]records.Upness, map[string]records.Upness) {
	jobsetsUp := make(map[string]records.Upness)
	jobsetNodesUp := make(map[string]records.Upness)

	uidMapKey := func(ns, name string) string {
		return fmt.Sprintf("%s/%s", ns, name)
	}
	jobsetUidMap := map[string]string{}

	for _, js := range jobsetList.Items {
		jobsetUid := string(js.UID)
		jobsetUidMap[uidMapKey(js.Namespace, js.Name)] = jobsetUid

		attrs := utils.ExtractJobSetAttrs(&js)
		specReplicas, readyReplicas := k8sutils.GetJobSetReplicas(&js)
		state, isTerminal := k8sutils.GetJobSetTerminalState(&js)
		expectedDown := isTerminal && state == jobset.JobSetCompleted
		failed := isTerminal && state == jobset.JobSetFailed

		jobsetsUp[jobsetUid] = records.Upness{
			ExpectedCount: specReplicas,
			ReadyCount:    readyReplicas,
			Attrs:         attrs,
			Status:        string(state),
			ExpectedDown:  expectedDown,
			Failed:        failed,
		}

		if !r.SliceEnabled {
			jobsetNodesUp[jobsetUid] = records.Upness{
				ExpectedCount: k8sutils.GetExpectedNodeCount(&js),
				Attrs:         attrs,
			}
		}
	}

	if !r.SliceEnabled {
		// jobset nodes should only be reconciled when slicing is disabled
		for _, node := range nodeList.Items {
			nodeStatus := k8sutils.IsNodeReady(&node)
			if jsNS, jsName := k8sutils.GetJobSetForNode(&node); jsNS != "" && jsName != "" {
				uid, ok := jobsetUidMap[uidMapKey(jsNS, jsName)]
				if !ok {
					continue
				}
				up, ok := jobsetNodesUp[uid]
				if !ok {
					continue
				}
				if nodeStatus == corev1.ConditionTrue {
					up.ReadyCount++
				} else if nodeStatus == corev1.ConditionUnknown {
					up.UnknownCount++
				}
				jobsetNodesUp[uid] = up
			}
		}
	}

	return jobsetsUp, jobsetNodesUp
}

// processLeaderWorkerSets extracts the upness status for all LeaderWorkerSets currently in the cluster.
// It parses the list of LWS resources to evaluate how many replicas are expected versus how many are currently ready,
// producing a map of raw upness metrics for the EventLog to persist.
func (r *WorkloadReconciler) processLeaderWorkerSets(lwsList lws.LeaderWorkerSetList) map[string]records.Upness {
	lwsUp := make(map[string]records.Upness)
	for _, lwsObj := range lwsList.Items {
		uid := string(lwsObj.UID)
		attrs := utils.ExtractLeaderWorkerSetAttrs(&lwsObj)
		expectedReplicas := int32(1)
		if lwsObj.Spec.Replicas != nil {
			expectedReplicas = *lwsObj.Spec.Replicas
		}
		lwsUp[uid] = records.Upness{
			ExpectedCount: expectedReplicas,
			ReadyCount:    lwsObj.Status.ReadyReplicas,
			Attrs:         attrs,
		}
	}
	return lwsUp
}

// processSlices evaluates the current state of Slices and their respective owners (e.g., JobSets).
// It distinguishes between expected downtime (where the owner is terminating/gone) and unexpected interruptions
// (where the Slice is deleted but the owner is still actively requesting it).
// It also handles persisting the SliceOwnerMap to a ConfigMap to preserve memory of slices across controller restarts.
func (r *WorkloadReconciler) processSlices(ctx context.Context, sliceList slicev1beta1.SliceList, sliceOwnerMap map[string]records.OwnerInfo) map[string]records.Upness {
	log := logf.FromContext(ctx).WithName("workload-reconciler")
	slicesUp := make(map[string]records.Upness)

	// observedSlices tracks the currently existing Slices in the cluster during this reconciliation.
	// We use this to compare against the ownerMap to detect if any Slices have been deleted
	// and need interruption tracking.
	observedSlices := make(map[string]bool)

	// Slices are reconciled by observing the current state of Slices and their owners.
	// We maintain a SliceOwnerMap to track the last known owner for each slice, allowing
	// us to distinguish between expected (owner gone/terminal) and unexpected downtime.
	sliceOwnerMapChanged := false
	for _, s := range sliceList.Items {
		observedSlices[s.Name] = true
		attrs := utils.ExtractSliceAttrs(&s)
		owner := records.OwnerInfo{
			Kind:      attrs.SliceOwnerKind,
			Namespace: attrs.SliceOwnerNamespace,
			Name:      attrs.SliceOwnerName,
		}
		if sliceOwnerMap[s.Name] != owner {
			sliceOwnerMap[s.Name] = owner
			sliceOwnerMapChanged = true
		}

		ownerExists, ownerTerminal, err := r.getOwnerStatus(ctx, attrs.SliceOwnerKind, attrs.SliceOwnerName, attrs.SliceOwnerNamespace)
		if err != nil {
			log.Error(err, "failed to get owner status", "slice", s.Name)
		}

		// isTerminating indicates if the Slice is in the process of being deleted.
		// (Note: DeletionTimestamp is set by the API server when a deletion is requested,
		// triggering a graceful termination phase if finalizers exist.)
		isTerminating := !s.DeletionTimestamp.IsZero()
		expectedDown := (!ownerExists || ownerTerminal)

		up := records.Upness{
			Attrs:         attrs,
			ExpectedCount: 1,
			ExpectedDown:  expectedDown,
		}

		// If the Slice is terminating (DeletionTimestamp is set), we treat it as Down (ReadyCount=0).
		// This logic is defined above where isTerminating = !s.DeletionTimestamp.IsZero()
		// If its owner is still active (expectedDown=false), this will correctly trigger an
		// interruption event in the Event Log.
		if !isTerminating {
			// Only update upness metrics for non-terminating Slices.
			for _, cond := range s.Status.Conditions {
				if cond.Type == slicev1beta1.SliceStateConditionType {
					switch cond.Status {
					case metav1.ConditionTrue:
						up.ReadyCount = 1
					case metav1.ConditionUnknown:
						up.UnknownCount = 1
					}
					break
				}
			}
		}
		slicesUp[s.Name] = up
	}

	// Reconcile observed slice state and SliceOwnerMap.
	// If a slice is in the map but not in the observed list, it has been deleted.
	// We then check if its owner is still active. If it is, the slice is marked
	// as down in the event log to record an interruption.
	for name, owner := range sliceOwnerMap {
		if !observedSlices[name] {
			ownerExists, ownerNotTerminal, err := r.getOwnerActiveStatus(ctx, owner.Kind, owner.Name, owner.Namespace)
			if err != nil {
				log.Error(err, "failed to get owner active status", "slice", name)
			}

			if ownerExists && ownerNotTerminal {
				// Slice is missing but owner is active -> mark as down in event log
				slicesUp[name] = records.Upness{
					Attrs: records.Attrs{
						SliceName:           name,
						SliceOwnerKind:      owner.Kind,
						SliceOwnerName:      owner.Name,
						SliceOwnerNamespace: owner.Namespace,
					},
					ExpectedCount: 1,
					ExpectedDown:  false, // It's an UNEXPECTED downtime
				}
			} else {
				// Owner is gone or not terminal -> delete from map
				delete(sliceOwnerMap, name)
				sliceOwnerMapChanged = true
			}
		}
	}

	// If the Slice-to-Owner mapping has changed during this reconciliation (e.g., new slices discovered
	// or deleted slices removed), persist the updated map back to the Kubernetes ConfigMap.
	if sliceOwnerMapChanged {
		data, err := json.Marshal(sliceOwnerMap)
		if err != nil {
			log.Error(err, "failed to marshal slice owner map")
		} else {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      r.SliceOwnerMapConfigMapRef.Name,
					Namespace: r.SliceOwnerMapConfigMapRef.Namespace,
				},
			}
			_, err = ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
				if configMap.Data == nil {
					configMap.Data = make(map[string]string)
				}
				configMap.Data[sliceOwnerMapKey] = string(data)
				return nil
			})
			if err != nil {
				log.Error(err, "failed to save slice owner map ConfigMap")
			}
		}
	}

	return slicesUp
}

func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podNamespace != "" && r.SliceOwnerMapConfigMapRef.Namespace != podNamespace {
		ctrl.Log.WithName(r.Name).Info("warning: SliceOwnerMapConfigMapRef is in a different namespace than the reconciler",
			"configMapNamespace", r.SliceOwnerMapConfigMapRef.Namespace,
			"reconcilerNamespace", podNamespace)
	}

	b := ctrl.NewControllerManagedBy(mgr).
		Named(r.Name).
		Watches(&jobset.JobSet{}, r.mapToStatic())

	if r.LeaderWorkerSetEnabled {
		b = b.Watches(&lws.LeaderWorkerSet{}, r.mapToStatic())
	}

	if r.SliceEnabled {
		b = b.Watches(&slicev1beta1.Slice{}, r.mapToStatic())
	} else {
		b = b.Watches(&corev1.Node{}, r.mapToStatic())
	}

	return b.Complete(r)
}

// mapToStatic generates a static event handler that maps any triggered event to a dummy reconciliation request.
// This ensures a full cluster-wide reconciliation of all workloads when any watched resource changes.
func (r *WorkloadReconciler) mapToStatic() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      "dummy-workload-name",
					Namespace: "default",
				},
			}}
		},
	)
}

// getOwnerStatus checks the cluster for a workload owner (e.g. JobSet, LeaderWorkerSet) by its kind, name, and namespace.
// Returns two booleans indicating if the owner exists and if it is in a terminal state, along with any error encountered.
func (r *WorkloadReconciler) getOwnerStatus(ctx context.Context, ownerKind, ownerName, ownerNamespace string) (exists bool, terminal bool, err error) {
	if ownerName == "" || ownerNamespace == "" {
		return false, false, nil
	}

	switch strings.ToLower(ownerKind) {
	case "jobset":
		var js jobset.JobSet
		if err := r.Get(ctx, types.NamespacedName{Name: ownerName, Namespace: ownerNamespace}, &js); err == nil {
			_, ownerTerminal := k8sutils.GetJobSetTerminalState(&js)
			return true, ownerTerminal, nil
		} else if !errors.IsNotFound(err) {
			return false, false, err
		}
	case "leaderworkerset":
		if r.LeaderWorkerSetEnabled {
			var lwsObj lws.LeaderWorkerSet
			if err := r.Get(ctx, types.NamespacedName{Name: ownerName, Namespace: ownerNamespace}, &lwsObj); err == nil {
				_, ownerTerminal := k8sutils.GetLeaderWorkerSetTerminalState(&lwsObj)
				return true, ownerTerminal, nil
			} else if !errors.IsNotFound(err) {
				return false, false, err
			}
		}
	}

	return false, false, nil
}

// getOwnerActiveStatus wraps getOwnerStatus to return whether an owner currently exists and is actively running.
// Returns two booleans indicating if the owner exists and if it is active (non-terminal), along with any error.
func (r *WorkloadReconciler) getOwnerActiveStatus(ctx context.Context, ownerKind, ownerName, ownerNamespace string) (exists bool, nonTerminal bool, err error) {
	var terminal bool
	exists, terminal, err = r.getOwnerStatus(ctx, ownerKind, ownerName, ownerNamespace)
	nonTerminal = !terminal
	return
}

/*
Copyright 2024 The Kubernetes authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"example.com/megamon/internal/k8sutils"
	"example.com/megamon/internal/records"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	slice "example.com/megamon/copied-slice-api/v1beta1"
)

const (
	SLICE_STATE_ACTIVATING                = "ACTIVATING"
	SLICE_STATE_ACTIVE                    = "ACTIVE"
	SLICE_STATE_ACTIVE_DEGRADED           = "ACTIVE_DEGRADED"
	SLICE_STATE_INCOMPLETE                = "INCOMPLETE"
	SLICE_STATE_FAILED                    = "FAILED"
	SLICE_STATE_UNKNOWN                   = "UNKNOWN"
	SLICE_STATE_HEALTH_STATUS_UNSPECIFIED = "HEALTH_STATUS_UNSPECIFIED"
)

var (
	replicatedJob_2x4_r1 = &jobset.ReplicatedJob{
		Name:     "rj-a",
		Replicas: 1,
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				Parallelism: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							k8sutils.NodeLabelGKETPUTopology: "2x4",
						},
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "busybox",
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		},
	}
	replicatedJob_2x4_r2 = &jobset.ReplicatedJob{
		Name:     "rj-b",
		Replicas: 2,
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				Parallelism: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{
							k8sutils.NodeLabelGKETPUTopology: "2x4",
						},
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "busybox",
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		},
	}
	jobsetSingleJob = &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "js-rj-8",
			Namespace: "default",
		},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{
				*replicatedJob_2x4_r1,
			},
		},
	}
	jobsetMultipleRJobs = &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "js-rj2-24",
			Namespace: "default",
		},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{
				*replicatedJob_2x4_r1,
				*replicatedJob_2x4_r2,
			},
		},
	}
)

var _ = Describe("Nodepool metrics", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var restCfg *rest.Config
	var k8sClient client.Client

	aggregationInterval := 1

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})
		metricsAddr = startManager(ctx, false, restCfg, aggregationInterval)
	})

	Context("When reconciling a resource", func() {
		jsRef := types.NamespacedName{
			Name:      "test-jobset",
			Namespace: "default",
		}

		jobRef := types.NamespacedName{
			Name:      "test-job",
			Namespace: "default",
		}

		nps, err := gkeClient.ListNodePools(ctx)
		Expect(err).To(BeNil(), "Failed to list node pools")

		var np = nps[0]

		var node = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
				Labels: map[string]string{
					"cloud.google.com/gke-nodepool":     nodePoolName,
					"cloud.google.com/gke-tpu-topology": "2x4",
				},
			},
		}

		var pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"jobset.sigs.k8s.io/jobset-name":           jsRef.Name,
					"batch.kubernetes.io/job-name":             jobRef.Name,
					"batch.kubernetes.io/job-completion-index": "0",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "busybox",
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				NodeName:      node.Name,
			},
		}

		It("should watch a Node", func() {
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
		})

		// Check metrics
		It("should watch a Pod", func() {
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		})

		// Check metrics
		It("should publish nodepool metrics", func() {
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				// Depends on node and jobset pod being created
				nodepool.job_scheduled.WithValue(1),
				// Only depend on nodepool being created
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(0),
				nodepool.recovery_count.WithValue(0),
				nodepool.up.WithValue(0),
				nodepool.up_time_seconds.WithValue(0),
				nodepool.tpu_chip_count.WithValue(256),
				nodepool.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateProvisioning),
			)
		})

		// update node to status ready
		It("should update first node to ready status", func() {
			node.Status = corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			}
			Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())

			// nodepool_up should still be 0; 16x16 topology expects 256
			By("rechecking the metrics for nodepool_up")
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				nodepool.job_scheduled.WithValue(1),
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(0),
				nodepool.recovery_count.WithValue(0),
				nodepool.up.WithValue(0),
				nodepool.up_time_seconds,
				nodepool.tpu_chip_count.WithValue(256),
				nodepool.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateProvisioning),
			)
		})

		// add in remaining 63 nodes
		nodeList := []*corev1.Node{}
		It("should succeed in adding all nodes", func() {
			var npErr error
			for i := range 63 {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("node-%d", i),
						Labels: map[string]string{
							"cloud.google.com/gke-nodepool":     nodePoolName,
							"cloud.google.com/gke-tpu-topology": tpuTopology,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				}
				nodeList = append(nodeList, node)
				if npErr = k8sClient.Create(ctx, node); npErr != nil {
					break
				}
			}
			Expect(npErr).To(BeNil())
		})

		// upness validation
		It("should update nodepool_up metric to 1 when all the nodes becomes Ready", func() {
			By("rechecking the metrics for nodepool_up")
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				nodepool.job_scheduled.WithValue(1),
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(0),
				nodepool.recovery_count.WithValue(0),
				nodepool.up.WithValue(1),
				nodepool.up_time_seconds,
				nodepool.tpu_chip_count.WithValue(256),
				nodepool.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateSuccess),
			)
		})

		It("should update nodepool_up metric to 0 when too many node status become Unknown", func() {
			By("updating 10 node status to Unknown")
			var updateErr error
			for i := range 10 {
				node := nodeList[i]
				node.Status = corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, LastHeartbeatTime: metav1.Now()},
					},
				}
				if updateErr = k8sClient.Status().Update(ctx, node); updateErr != nil {
					break
				}
			}
			Expect(updateErr).To(BeNil())

			By("rechecking the metrics for nodepool_up")
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				nodepool.job_scheduled.WithValue(1),
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(1),
				nodepool.recovery_count,
				nodepool.up.WithValue(0),
				nodepool.up_time_seconds,
				nodepool.tpu_chip_count.WithValue(256),
			)
		})

		// update nodepool to have +4 more nodes in READY state, so we have 6 nodes in UNKNOWN
		It("should update nodepool_up metric to 1 when less than 10% of node status become Unknown", func() {
			By("updating 4 node status to Ready")
			var updateErr error
			for i := range 4 {
				node := nodeList[i]
				node.Status = corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: metav1.Now()},
					},
				}
				if updateErr = k8sClient.Status().Update(ctx, node); updateErr != nil {
					break
				}
			}
			Expect(updateErr).To(BeNil())

			By("rechecking the metrics for nodepool_up")
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				nodepool.job_scheduled.WithValue(1),
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(1),
				nodepool.recovery_count,
				nodepool.up.WithValue(1),
				nodepool.up_time_seconds,
				nodepool.tpu_chip_count.WithValue(256),
			)
		})

	})
})

var _ = Describe("JobSet metrics", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var restCfg *rest.Config
	var k8sClient client.Client
	aggregationInterval := 1
	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})
		metricsAddr = startManager(ctx, false, restCfg, aggregationInterval)
	})

	Context("When reconciling a resource", func() {
		js := jobsetSingleJob.DeepCopy()
		js.ResourceVersion = ""
		js.UID = ""
		It("should watch a JobSet", func() {
			Expect(k8sClient.Create(ctx, js)).To(Succeed())
		})

		It("should publish metrics after submitting a jobset", func() {
			jobset := expectedMetricsForJobSet(js, "2x4")
			assertMetrics(metricsAddr,
				jobset.up.WithValue(0),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.interruption_count.WithValue(0),
				jobset.recovery_count.WithValue(0),
				jobset.tpu_chip_count.WithValue(8),
				jobset.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateProvisioning),
			)
		})

		It("should publish updated metrics after jobset is first marked as ready for the first time", func() {
			By("updating the ready jobs in the jobset status")
			js.Status.ReplicatedJobsStatus = []jobset.ReplicatedJobStatus{
				{
					Name:  js.Spec.ReplicatedJobs[0].Name,
					Ready: 1,
				},
			}
			Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

			By("rechecking the metrics")
			jobset := expectedMetricsForJobSet(js, "2x4")
			assertMetrics(metricsAddr,
				jobset.up.WithValue(1),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.down_time_initial_seconds,
				jobset.interruption_count.WithValue(0),
				jobset.recovery_count.WithValue(0),
				jobset.tpu_chip_count.WithValue(8),
				jobset.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateSuccess),
			)
		})

		It("should publish updated metrics after jobset is interrupted for the first time", func() {
			By("updating the unready jobs in the jobset status")
			js.Status.ReplicatedJobsStatus = []jobset.ReplicatedJobStatus{
				{
					Name:  js.Spec.ReplicatedJobs[0].Name,
					Ready: 0,
				},
			}
			Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

			By("rechecking the metrics")
			jobset := expectedMetricsForJobSet(js, "2x4")
			assertMetrics(metricsAddr,
				jobset.up.WithValue(0),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.down_time_initial_seconds,
				jobset.up_time_between_interruption_seconds,
				jobset.up_time_between_interruption_mean_seconds,
				jobset.up_time_between_interruption_latest_seconds,
				jobset.interruption_count.WithValue(1),
				jobset.recovery_count.WithValue(0),
				jobset.tpu_chip_count.WithValue(8),
			)
		})

		It("should publish updated metrics after jobset recovers for the first time", func() {
			By("updating the unready jobs in the jobset status")
			js.Status.ReplicatedJobsStatus = []jobset.ReplicatedJobStatus{
				{
					Name:  js.Spec.ReplicatedJobs[0].Name,
					Ready: 1,
				},
			}
			Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

			By("rechecking the metrics")
			jobset := expectedMetricsForJobSet(js, "2x4")
			assertMetrics(metricsAddr,
				jobset.up.WithValue(1),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.down_time_initial_seconds,
				jobset.up_time_between_interruption_seconds,
				jobset.up_time_between_interruption_mean_seconds,
				jobset.up_time_between_interruption_latest_seconds,
				jobset.down_time_between_recovery_seconds,
				jobset.down_time_between_recovery_mean_seconds,
				jobset.down_time_between_recovery_latest_seconds,
				jobset.interruption_count.WithValue(1),
				jobset.recovery_count.WithValue(1),
				jobset.tpu_chip_count.WithValue(8),
			)
		})

		It("should publish build info metric", func() {
			By("checking for megamon_build_info metric")
			metrics := expectedMetricPrefix + "_build_info{commit=\"none\",date=\"unknown\",otel_scope_name=\"megamon\",otel_scope_version=\"\",version=\"dev\"} 1"
			Eventually(func() (string, error) {
				return fetchMetrics(metricsAddr)
			}, "3s", "10ms").Should(ContainSubstring(metrics))
		})

		It("should NOT increment interruption count when jobset completes (expected downtime)", func() {
			By("setting the jobset status to Completed")
			js.Status.TerminalState = string(jobset.JobSetCompleted)
			js.Status.Conditions = []metav1.Condition{
				{
					Type:               string(jobset.JobSetCompleted),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "AllJobsCompleted",
					Message:            "jobset completed",
				},
			}
			Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

			By("checking that interruption count is still 1")
			// We iterate a few times to ensure the aggregator picks it up and DOES NOT increment
			metrics := expectedMetricsForJobSet(js, "2x4")

			// 1. Wait for the aggregator to pick up the "Completed" state (Up -> 0)
			Eventually(func(g Gomega) {
				m, err := fetchMetrics(metricsAddr)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(m).To(ContainSubstring(metrics.up.WithValue(0).String()))
			}, "3s", "10ms").Should(Succeed())

			// 2. Ensure Interruption Count remains 1 (Expected Downtime should NOT increment it)
			Consistently(func(g Gomega) {
				m, err := fetchMetrics(metricsAddr)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(m).To(ContainSubstring(metrics.interruption_count.WithValue(1).String()))
			}, "3s", "10ms").Should(Succeed())
		})

		It("should watch a jobset with a two replicated jobs", func() {
			Expect(k8sClient.Create(ctx, jobsetMultipleRJobs)).To(Succeed())
		})
		It("should publish total TPU chip counts by jobset with multiple replicated jobs with >1 replica", func() {
			By("looking at TPU topology per replicated job in a deployed jobset")
			metrics := expectedMetricsForJobSet(jobsetMultipleRJobs, "2x4")
			assertMetrics(metricsAddr,
				metrics.tpu_chip_count.WithValue(24),
			)
		})
	})
})

var _ = Describe("JobSet Node metrics absent when slice is enabled", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var restCfg *rest.Config
	aggregationInterval := 1

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, _ = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})
		metricsAddr = startManager(ctx, true, restCfg, aggregationInterval)
	})

	It("should not publish any jobset node metrics when slice is enabled", func() {
		By("checking that no jobset node metrics are published")
		unexpectedMetricPrefix := "jobset_node_"
		Eventually(func() (string, error) {
			return fetchMetrics(metricsAddr)
		}, "3s", "10ms").ShouldNot(ContainSubstring(unexpectedMetricPrefix))
	})
})

var _ = Describe("JobSet failed provisioning metrics", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var restCfg *rest.Config
	var k8sClient client.Client
	aggregationInterval := 1
	var js *jobset.JobSet

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})
		metricsAddr = startManager(ctx, false, restCfg, aggregationInterval)

		js = jobsetSingleJob.DeepCopy()
		js.Name = "failed-jobset"
		js.Namespace = "default"
		Expect(k8sClient.Create(ctx, js)).To(Succeed())
	})

	It("should publish failed provisioning metrics when jobset fails before becoming ready", func() {
		By("setting the terminal state to Failed")
		js.Status.TerminalState = string(jobset.JobSetFailed)
		js.Status.Conditions = []metav1.Condition{
			{
				Type:               string(jobset.JobSetFailed),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "PodsFailed",
				Message:            "jobset failed",
			},
		}
		Expect(k8sClient.Status().Update(ctx, js)).To(Succeed())

		By("checking that failed provisioning duration is published")
		metrics := expectedMetricsForJobSet(js, "2x4")
		assertMetrics(metricsAddr,
			metrics.provisioning_duration.WithLabel("provisioning_state", records.ProvisioningStateFailed),
			metrics.down_time_initial_seconds,
		)
	})
})

type upnessMetrics struct {
	// Always present
	up                 metric
	up_time_seconds    metric
	down_time_seconds  metric
	interruption_count metric
	recovery_count     metric
	tpu_chip_count     metric

	// Present after events occur
	up_time_between_interruption_seconds        metric
	up_time_between_interruption_mean_seconds   metric
	up_time_between_interruption_latest_seconds metric
	down_time_initial_seconds                   metric
	down_time_between_recovery_seconds          metric
	down_time_between_recovery_mean_seconds     metric
	down_time_between_recovery_latest_seconds   metric
	provisioning_duration                       metric
}

type utilizationMetrics struct {
	// Always present
	down_time_seconds  metric
	interruption_count metric
	recovery_count     metric
	up                 metric
	up_time_seconds    metric
	tpu_chip_count     metric

	// Present after events occur
	job_scheduled         metric
	provisioning_duration metric
}

func expectedMetricsForNodePool(np *containerv1beta1.NodePool, jobSetName string, jobName string, sliceName string) utilizationMetrics {
	nodepoolLabels := map[string]interface{}{
		"nodepool_name":   np.Name,
		"tpu_topology":    tpuTopology,
		"tpu_accelerator": tpuAccelerator,
	}
	if sliceName != "" {
		nodepoolLabels["slice_name"] = sliceName
	}
	nodepoolJobLabels := map[string]interface{}{
		"job_name":      jobName,
		"jobset_name":   jobSetName,
		"nodepool_name": np.Name,
	}
	return utilizationMetrics{
		job_scheduled: metric{
			name:   "nodepool_job_scheduled",
			labels: nodepoolJobLabels,
		},
		down_time_seconds: metric{
			name:   "nodepool_down_time_seconds",
			labels: nodepoolLabels,
		},
		interruption_count: metric{
			name:   "nodepool_interruption_count",
			labels: nodepoolLabels,
		},
		recovery_count: metric{
			name:   "nodepool_recovery_count",
			labels: nodepoolLabels,
		},
		up: metric{
			name:   "nodepool_up",
			labels: nodepoolLabels,
		},
		up_time_seconds: metric{
			name:   "nodepool_up_time_seconds",
			labels: nodepoolLabels,
		},
		tpu_chip_count: metric{
			name:   "nodepool_tpu_chip_count",
			labels: nodepoolLabels,
		},
		provisioning_duration: metric{
			name:   "nodepool_provisioning_duration_seconds",
			labels: nodepoolLabels,
		},
	}
}

func expectedMetricsForJobSet(js *jobset.JobSet, tpuTopology string) upnessMetrics {
	return expectedMetricsForJobSetWithSlice(js, tpuTopology, nil)
}

func expectedMetricsForJobSetWithSlice(js *jobset.JobSet, tpuTopology string, sl *slice.Slice) upnessMetrics {
	jsLabels := map[string]interface{}{
		"jobset_name":      js.Name,
		"jobset_namespace": js.Namespace,
		"jobset_uid":       js.UID,
		"tpu_topology":     tpuTopology,
	}
	if sl != nil {
		if sl.Name != "" {
			jsLabels["slice_name"] = sl.Name
		}
	}
	return upnessMetrics{
		up: metric{
			name:   "jobset_up",
			labels: jsLabels,
		},
		interruption_count: metric{
			name:   "jobset_interruption_count",
			labels: jsLabels,
		},
		recovery_count: metric{
			name:   "jobset_recovery_count",
			labels: jsLabels,
		},
		up_time_seconds: metric{
			name:   "jobset_up_time_seconds",
			labels: jsLabels,
		},
		down_time_seconds: metric{
			name:   "jobset_down_time_seconds",
			labels: jsLabels,
		},
		up_time_between_interruption_seconds: metric{
			name:   "jobset_up_time_between_interruption_seconds",
			labels: jsLabels,
		},
		up_time_between_interruption_mean_seconds: metric{
			name:   "jobset_up_time_between_interruption_mean_seconds",
			labels: jsLabels,
		},
		up_time_between_interruption_latest_seconds: metric{
			name:   "jobset_up_time_between_interruption_latest_seconds",
			labels: jsLabels,
		},
		down_time_initial_seconds: metric{
			name:   "jobset_down_time_initial_seconds",
			labels: jsLabels,
		},
		down_time_between_recovery_seconds: metric{
			name:   "jobset_down_time_between_recovery_seconds",
			labels: jsLabels,
		},
		down_time_between_recovery_mean_seconds: metric{
			name:   "jobset_down_time_between_recovery_mean_seconds",
			labels: jsLabels,
		},
		down_time_between_recovery_latest_seconds: metric{
			name:   "jobset_down_time_between_recovery_latest_seconds",
			labels: jsLabels,
		},
		tpu_chip_count: metric{
			name:   "jobset_tpu_chip_count",
			labels: jsLabels,
		},
		provisioning_duration: metric{
			name:   "jobset_provisioning_duration_seconds",
			labels: jsLabels,
		},
	}
}

func updateSliceStatus(s *slice.Slice, reason string, status metav1.ConditionStatus) {
	if len(s.Status.Conditions) == 0 {
		s.Status.Conditions = []metav1.Condition{{Type: slice.SliceStateConditionType}}
	}
	s.Status.Conditions[0].Type = slice.SliceStateConditionType
	s.Status.Conditions[0].Reason = reason
	s.Status.Conditions[0].Status = status
	s.Status.Conditions[0].LastTransitionTime = metav1.Now()
}

func expectedMetricsForSlice(s *slice.Slice) upnessMetrics {
	sLabels := map[string]interface{}{
		"slice_name":            s.Name,
		"slice_owner_name":      s.Labels["tpu-provisioner.cloud.google.com/owner-name"],
		"slice_owner_namespace": s.Labels["tpu-provisioner.cloud.google.com/owner-namespace"],
		"slice_owner_kind":      s.Labels["tpu-provisioner.cloud.google.com/owner-kind"],
		"tpu_accelerator":       string(s.Spec.Type),
		"tpu_topology":          s.Spec.Topology,
	}
	chipCount, _ := k8sutils.GetTpuTopologyToChipCount(s.Spec.Topology)
	return upnessMetrics{
		up:                                   metric{name: "slice_up", labels: sLabels},
		interruption_count:                   metric{name: "slice_interruption_count", labels: sLabels},
		recovery_count:                       metric{name: "slice_recovery_count", labels: sLabels},
		up_time_seconds:                      metric{name: "slice_up_time_seconds", labels: sLabels},
		down_time_seconds:                    metric{name: "slice_down_time_seconds", labels: sLabels},
		tpu_chip_count:                       metric{name: "slice_tpu_chip_count", labels: sLabels}.WithValue(chipCount),
		up_time_between_interruption_seconds: metric{name: "slice_up_time_between_interruption_seconds", labels: sLabels},
		up_time_between_interruption_mean_seconds:   metric{name: "slice_up_time_between_interruption_mean_seconds", labels: sLabels},
		up_time_between_interruption_latest_seconds: metric{name: "slice_up_time_between_interruption_latest_seconds", labels: sLabels},
		down_time_initial_seconds:                   metric{name: "slice_down_time_initial_seconds", labels: sLabels},
		down_time_between_recovery_seconds:          metric{name: "slice_down_time_between_recovery_seconds", labels: sLabels},
		down_time_between_recovery_mean_seconds:     metric{name: "slice_down_time_between_recovery_mean_seconds", labels: sLabels},
		down_time_between_recovery_latest_seconds:   metric{name: "slice_down_time_between_recovery_latest_seconds", labels: sLabels},
		provisioning_duration:                       metric{name: "slice_provisioning_duration_seconds", labels: sLabels},
	}
}

var _ = Describe("Slice deletion (planned)", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var s *slice.Slice
	var restCfg *rest.Config
	var k8sClient client.Client

	const aggregationTime = 2

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})

		metricsAddr = startManager(ctx, true, restCfg, aggregationTime)
		s = &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name: "slice",
				Labels: map[string]string{
					"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
					"tpu-provisioner.cloud.google.com/owner-namespace": "default",
					"tpu-provisioner.cloud.google.com/owner-kind":      "JobSet",
				},
			},
			Spec: slice.SliceSpec{
				Type:         slice.TypeTpu7x,
				Topology:     "2x2x2",
				PartitionIds: []string{"p1"},
			},
		}
	})

	It("should not mark a slice deletion as an interruption (if planned)", func() {
		By("creating the JobSet owner")
		js := jobsetSingleJob.DeepCopy()
		js.ResourceVersion = ""
		js.UID = ""
		js.Name = "test-owner"
		js.Namespace = "default"
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		By("creating a Slice")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		sliceMetrics := expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0))

		By("updating the slice READY condition == True, slice state == ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		// Refresh metrics to include slice_state
		By("verify slice metric shows up now")
		sliceMetrics = expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1))

		By("deleting the JobSet owner")
		js.ResourceVersion = "" // Reset for deletion just in case
		Expect(k8sClient.Delete(ctx, js)).To(Succeed())

		By("waiting for JobSet to be deleted")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: js.Name}, js)
		}, "3s", "10ms").ShouldNot(Succeed())

		By("deleting the slice")
		Expect(k8sClient.Delete(ctx, s)).To(Succeed())

		// Wait for deletion to complete
		By("waiting for slice to be deleted")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: s.Name}, s)
		}, "3s", "10ms").ShouldNot(Succeed())

		By("verifying metrics are gone immediately since owner is gone")
		assertMetricsAbsent(metricsAddr, sliceMetrics.up)
	})
})

var _ = Describe("Slice deletion (unplanned)", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var s *slice.Slice
	var restCfg *rest.Config
	var k8sClient client.Client

	const aggregationTime = 2

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})

		metricsAddr = startManager(ctx, true, restCfg, aggregationTime)
		s = &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name: "slice",
				Labels: map[string]string{
					"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
					"tpu-provisioner.cloud.google.com/owner-namespace": "default",
					"tpu-provisioner.cloud.google.com/owner-kind":      "JobSet",
				},
			},
			Spec: slice.SliceSpec{
				Type:         slice.TypeTpu7x,
				Topology:     "2x2x2",
				PartitionIds: []string{"p1"},
			},
		}
	})
	It("should not mark a slice deletion as interruption (if unplanned)", func() {
		By("creating the JobSet owner")
		js := jobsetSingleJob.DeepCopy()
		js.ResourceVersion = ""
		js.UID = ""
		js.Name = "test-owner"
		js.Namespace = "default"
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		By("creating a Slice")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		sliceMetrics := expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0))

		By("updating the slice READY condition == True, slice state == ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		// Refresh metrics, should show as up
		sliceMetrics = expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1))

		// Set slice Ready to Flase
		By("updating slice READY condition == False")
		updateSliceStatus(s, SLICE_STATE_FAILED, metav1.ConditionFalse)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		sliceMetrics = expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0), sliceMetrics.interruption_count.WithValue(1))

		By("deleting the slice")
		Expect(k8sClient.Delete(ctx, s)).To(Succeed())

		// Wait for deletion to complete
		By("waiting for slice to be deleted")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: s.Name}, s)
		}, "3s", "10ms").ShouldNot(Succeed())

		By("deleting the JobSet owner")
		js.ResourceVersion = ""
		Expect(k8sClient.Delete(ctx, js)).To(Succeed())

		By("verifying metrics are gone immediately since owner is gone")
		assertMetricsAbsent(metricsAddr, sliceMetrics.up)
	})
})

var _ = Describe("Slice repair", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var s *slice.Slice
	var restCfg *rest.Config
	var k8sClient client.Client

	const aggregationTime = 2

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			stopManager(cancel, metricsAddr)
		})

		metricsAddr = startManager(ctx, true, restCfg, aggregationTime)
		s = &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name: "slice",
				Labels: map[string]string{
					"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
					"tpu-provisioner.cloud.google.com/owner-namespace": "default",
					"tpu-provisioner.cloud.google.com/owner-kind":      "JobSet",
				},
			},
			Spec: slice.SliceSpec{
				Type:         slice.TypeTpu7x,
				Topology:     "2x2x2",
				PartitionIds: []string{"p1"},
			},
		}
	})
	It("show an interruption and recovery on slice re-creation with same name within grace period", func() {
		By("creating the JobSet owner")
		js := jobsetSingleJob.DeepCopy()
		js.ResourceVersion = ""
		js.UID = ""
		js.Name = "test-owner"
		js.Namespace = "default"
		Expect(k8sClient.Create(ctx, js)).To(Succeed())

		By("creating a Slice")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		// Allow time for aggregation
		sliceMetrics := expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0))

		By("updating the slice READY condition == True, slice state == ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		// Refresh metrics, should show as up
		sliceMetrics = expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1))

		// Set slice Ready to Flase
		By("updating slice READY condition == False")
		updateSliceStatus(s, SLICE_STATE_FAILED, metav1.ConditionFalse)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		sliceMetrics = expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0), sliceMetrics.interruption_count.WithValue(1))

		// repair flow, scheduler observes slice READY==false
		// scheduler deletes slice and recreates with same name with healthy partitions
		By("deleting the slice")
		Expect(k8sClient.Delete(ctx, s)).To(Succeed())

		// Wait for deletion to complete
		By("waiting for slice to be deleted")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: s.Name}, s)
		}, "3s", "10ms").ShouldNot(Succeed())

		By("re-creating slice with same name")
		s.ResourceVersion = "" // Reset for recreation
		s.UID = ""
		s.DeletionTimestamp = nil
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		By("updating the slice READY condition == True, slice state == ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

		By("verifying metrics show up and history is preserved (not pruned)")
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1), sliceMetrics.interruption_count.WithValue(1), sliceMetrics.recovery_count.WithValue(1))
	})

})

func fetchMetrics(addr string) (string, error) {
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func assertMetrics(addr string, expected ...metric) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		metrics, err := fetchMetrics(addr)
		g.Expect(err).NotTo(HaveOccurred())
		for _, exp := range expected {
			g.Expect(metrics).To(ContainSubstring(exp.name+"{"), "metric name not found: %s", exp.name)
			g.Expect(metrics).To(ContainSubstring(exp.String()), "full metric does not match: %s", exp.String())
		}
	}, "5s", "100ms").Should(Succeed())
}

func assertMetricsAbsent(addr string, expected ...metric) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		metrics, err := fetchMetrics(addr)
		g.Expect(err).NotTo(HaveOccurred())
		for _, exp := range expected {
			g.Expect(metrics).NotTo(ContainSubstring(exp.name+"{"), "metric found but should be absent: %s", exp.name)
		}
	}, "5s", "100ms").Should(Succeed())
}

type metric struct {
	name   string
	labels map[string]any
	value  any
}

func (m metric) WithValue(val any) metric {
	cp := m
	cp.value = val
	return cp
}

func (m metric) WithLabel(k string, v any) metric {
	cp := m
	cp.labels = make(map[string]any, len(m.labels)+1)
	maps.Copy(cp.labels, m.labels)
	cp.labels[k] = v
	return cp
}

func (m metric) String() string {
	if m.value != nil {
		return m.valueString(m.value)
	}
	return m.valuelessString()
}

func (m metric) valueString(val any) string {
	return fmt.Sprintf("%s %v", m.valuelessString(), val)
}

// valuelessString returns the string representation of the metric without its value,
// including its name and formatted labels.
func (m metric) valuelessString() string {
	labels := make(map[string]any, len(m.labels)+2)
	maps.Copy(labels, m.labels)
	labels["otel_scope_name"] = "megamon"
	labels["otel_scope_version"] = ""

	var parts []string
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		parts = append(parts, fmt.Sprintf("%s=\"%v\"", k, labels[k]))
	}
	return fmt.Sprintf("%s_%s{%s}", expectedMetricPrefix, m.name, strings.Join(parts, ","))
}

var _ = Describe("Event Summarization Logic", func() {
	It("should correctly summarize a flow with expected downtime", func() {
		// This uses the internal/records logic directly, effectively a unit test
		// but placed here as requested by feedback.

		// Start time: T0
		t0, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
		ctx := context.Background()

		// 1. Initialize empty record
		var rec records.EventRecords

		// 2. T0: Component starts (Not Up yet)
		records.AppendUpEvent(t0, &rec, false, false, false)

		// 3. T+10m: Component becomes Ready (Up)
		t1 := t0.Add(10 * time.Minute)
		records.AppendUpEvent(t1, &rec, true, false, false)

		// 4. T+30m: Component goes into EXPECTED maintenance
		// This should NOT count as an interruption.
		t2 := t0.Add(30 * time.Minute)
		records.AppendUpEvent(t2, &rec, false, true, false) // isUp=false, expected=true, failed=false

		// 5. T+60m: Component comes back Up (no recovery)
		t3 := t0.Add(60 * time.Minute)
		records.AppendUpEvent(t3, &rec, true, false, false)

		// 6. T+90m: Component crashes (UNPLANNED down)
		// This SHOULD count as an interruption.
		t4 := t0.Add(90 * time.Minute)
		records.AppendUpEvent(t4, &rec, false, false, false) // isUp=false, expected=false, failed=false

		// 7. T+100m: Component recovers
		t5 := t0.Add(100 * time.Minute)
		records.AppendUpEvent(t5, &rec, true, false, false)

		// Verify at T+120m
		now := t0.Add(120 * time.Minute)
		summary := rec.Summarize(ctx, now)

		// 1. Interruption Count should be exactly 1 (the crash at T+90m).
		Expect(summary.InterruptionCount).To(Equal(1), "InterruptionCount mismatch")

		// 2. Recovery Count should be 1.
		Expect(summary.RecoveryCount).To(Equal(1), "RecoveryCount mismatch")

		// 3. Check Downtime Durations
		// Initial Down: t0 -> t1 = 10m
		Expect(summary.DownTimeInitial).To(Equal(10*time.Minute), "DownTimeInitial mismatch")
		Expect(summary.ProvisioningDuration).To(Equal(10*time.Minute), "ProvisioningDuration mismatch")
		Expect(summary.ProvisioningState).To(Equal("success"), "ProvisioningState mismatch")

		// Total Down Time: 10m + 30m + 10m = 50m
		Expect(summary.DownTime).To(Equal(50*time.Minute), "Total DownTime mismatch")

		// Total Up Time: 20m + 30m + 20m = 70m
		Expect(summary.UpTime).To(Equal(70*time.Minute), "Total UpTime mismatch")
	})

	// Added to test provisioning duration for resources that never become UP.
	It("should correctly summarize a flow that never becomes UP", func() {
		t0, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
		ctx := context.Background()
		var rec records.EventRecords

		// 1. T0: Component starts (Not Up yet)
		records.AppendUpEvent(t0, &rec, false, false, false)

		// Verify at T+30m (still provisioning)
		now := t0.Add(30 * time.Minute)
		summary := rec.Summarize(ctx, now)

		Expect(summary.ProvisioningDuration).To(Equal(30*time.Minute), "ProvisioningDuration mismatch")
		Expect(summary.ProvisioningState).To(Equal(records.ProvisioningStateProvisioning), "ProvisioningState mismatch")
		Expect(summary.DownTimeInitial).To(Equal(time.Duration(0)), "DownTimeInitial mismatch")
	})

	It("should correctly summarize a failed provisioning flow", func() {
		t0, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
		ctx := context.Background()
		var rec records.EventRecords

		// 1. T0: Component starts (Not Up yet)
		records.AppendUpEvent(t0, &rec, false, false, false)

		// 2. T+10m: Component fails
		records.AppendUpEvent(t0.Add(10*time.Minute), &rec, false, false, true)

		now := t0.Add(30 * time.Minute)
		summary := rec.Summarize(ctx, now)

		Expect(summary.ProvisioningDuration).To(Equal(10*time.Minute), "ProvisioningDuration mismatch")
		Expect(summary.ProvisioningState).To(Equal(records.ProvisioningStateFailed), "ProvisioningState mismatch")
		Expect(summary.DownTimeInitial).To(Equal(10*time.Minute), "DownTimeInitial mismatch")
	})
})

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
	"net/http"
	"sort"
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

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			cancel()
			time.Sleep(3 * time.Second) // Wait for manager shutdown
		})
		metricsAddr = startManager(ctx, false, restCfg)
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

		// Necessary because pod reconciler uses a cached client
		// which is eventually consistent w k8sClient here
		time.Sleep(5 * time.Second)

		It("should watch a Pod", func() {
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		})

		// Necessary because pod reconciler uses a cached client
		// which is eventually consistent w k8sClient here
		time.Sleep(5 * time.Second)

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
			time.Sleep(3 * time.Second)
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name, "")
			assertMetrics(metricsAddr,
				nodepool.job_scheduled.WithValue(1),
				nodepool.down_time_seconds,
				nodepool.interruption_count.WithValue(0),
				nodepool.recovery_count.WithValue(0),
				nodepool.up.WithValue(0),
				nodepool.up_time_seconds,
				nodepool.tpu_chip_count.WithValue(256),
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
			// Allow time for aggregation (1s interval) and metric update
			time.Sleep(3 * time.Second)

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

			// Allow time for aggregation (1s interval) and metric update
			time.Sleep(3 * time.Second)

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

			// Allow time for aggregation (1s interval) and metric update
			time.Sleep(3 * time.Second)

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

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			cancel()
			time.Sleep(3 * time.Second) // Wait for manager shutdown
		})
		metricsAddr = startManager(ctx, false, restCfg)
	})

	Context("When reconciling a resource", func() {
		js := jobsetSingleJob
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
			}, "5s", "1s").Should(ContainSubstring(metrics))
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
			}, "10s", "1s").Should(Succeed())

			// 2. Ensure Interruption Count remains 1 (Expected Downtime should NOT increment it)
			Consistently(func(g Gomega) {
				m, err := fetchMetrics(metricsAddr)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(m).To(ContainSubstring(metrics.interruption_count.WithValue(1).String()))
			}, "5s", "1s").Should(Succeed())
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

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, _ = startTestEnv()
		DeferCleanup(func() {
			cancel()
			time.Sleep(3 * time.Second) // Wait for manager shutdown
		})
		metricsAddr = startManager(ctx, true, restCfg)
	})

	It("should not publish any jobset node metrics when slice is enabled", func() {
		By("checking that no jobset node metrics are published")
		unexpectedMetricPrefix := "jobset_node_"
		Eventually(func() (string, error) {
			return fetchMetrics(metricsAddr)
		}, "5s", "1s").ShouldNot(ContainSubstring(unexpectedMetricPrefix))
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
	job_scheduled metric
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

var _ = Describe("Slice Metrics Scenarios", func() {
	sliceLifecycleTest := func(enableSlice bool) {
		var ctx context.Context
		var cancel context.CancelFunc
		var metricsAddr string
		var s *slice.Slice
		var restCfg *rest.Config
		var k8sClient client.Client

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			_, restCfg, k8sClient = startTestEnv()
			DeferCleanup(func() {
				cancel()
				time.Sleep(3 * time.Second) // Wait for manager shutdown
			})

			metricsAddr = startManager(ctx, enableSlice, restCfg)
			s = &slice.Slice{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("test-slice-%v", enableSlice),
					Labels: map[string]string{
						"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
						"tpu-provisioner.cloud.google.com/owner-namespace": "default",
						"tpu-provisioner.cloud.google.com/owner-kind":      "test-kind",
					},
				},
				Spec: slice.SliceSpec{
					Type:         slice.TypeTpu7x,
					Topology:     "2x2x2",
					PartitionIds: []string{"p1"},
				},
			}
		})

		It("should verify slice lifecycle", func() {
			By("watching a Slice")
			Expect(k8sClient.Create(ctx, s)).To(Succeed())

			time.Sleep(3 * time.Second)
			sliceMetrics := expectedMetricsForSliceWithState(s, "")
			if enableSlice {
				assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0), sliceMetrics.tpu_chip_count)
			} else {
				assertMetricsAbsent(metricsAddr, sliceMetrics.tpu_chip_count)
			}

			By("updating the slice status to READY with reason ACTIVE")
			updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
			time.Sleep(3 * time.Second)

			sliceMetrics = expectedMetricsForSliceWithState(s, "ACTIVE")
			if enableSlice {
				assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1), sliceMetrics.tpu_chip_count, sliceMetrics.down_time_initial_seconds)
			} else {
				assertMetricsAbsent(metricsAddr, sliceMetrics.up)
			}

			By("updating the slice status to READY with reason ACTIVE_DEGRADED")
			updateSliceStatus(s, SLICE_STATE_ACTIVE_DEGRADED, metav1.ConditionTrue)
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
			time.Sleep(3 * time.Second)

			sliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVE_DEGRADED)
			if enableSlice {
				assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1), sliceMetrics.tpu_chip_count, sliceMetrics.down_time_initial_seconds, sliceMetrics.interruption_count.WithValue(0))
			} else {
				assertMetricsAbsent(metricsAddr, sliceMetrics.up)
			}

			By("updating the slice status to NOT_READY with reason INCOMPLETE")
			updateSliceStatus(s, SLICE_STATE_INCOMPLETE, metav1.ConditionFalse)
			sliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_INCOMPLETE)
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
			time.Sleep(3 * time.Second)

			if enableSlice {
				assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0), sliceMetrics.interruption_count.WithValue(1))
			} else {
				assertMetricsAbsent(metricsAddr, sliceMetrics.interruption_count)
			}

			By("deleting the slice")
			Expect(k8sClient.Delete(ctx, s)).To(Succeed())
			time.Sleep(5 * time.Second)

			if enableSlice {
				assertMetricsAbsent(metricsAddr, sliceMetrics.up)
			} else {
				assertMetricsAbsent(metricsAddr, sliceMetrics.up)
			}
		})
	}

	Context("With Slice Disabled", func() {
		sliceLifecycleTest(false)
	})

	Context("With Slice Enabled", func() {
		sliceLifecycleTest(true)
	})
})

var _ = Describe("Slice Deletion and Recreation", func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var s *slice.Slice
	var restCfg *rest.Config
	var k8sClient client.Client
	const gracePeriod = 5 * time.Second

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			cancel()
			time.Sleep(3 * time.Second) // Wait for manager shutdown
		})

		metricsAddr = startManager(ctx, true, restCfg, gracePeriod)
		s = &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name: "slice",
				Labels: map[string]string{
					"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
					"tpu-provisioner.cloud.google.com/owner-namespace": "default",
					"tpu-provisioner.cloud.google.com/owner-kind":      "test-kind",
				},
			},
			Spec: slice.SliceSpec{
				Type:         slice.TypeTpu7x,
				Topology:     "2x2x2",
				PartitionIds: []string{"p1"},
			},
		}
	})

	It("should preserve slice metrics during the grace period after deletion", func() {
		By("creating a Slice")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		// Allow time for aggregation
		time.Sleep(3 * time.Second)
		sliceMetrics := expectedMetricsForSlice(s)
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0))

		By("updating the slice status to ready")
		updateSliceStatus(s, "SliceReady", metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)

		// Refresh metrics to include slice_state
		sliceMetrics = expectedMetricsForSliceWithState(s, "SliceReady")
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1))

		By("deleting the slice")
		Expect(k8sClient.Delete(ctx, s)).To(Succeed())

		By("verifying metrics are still present during grace period")
		// Metric should show 'up=0' because it's missing from live state
		// but its record is preserved in history.
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(0))

		By("waiting for grace period to expire")
		time.Sleep(gracePeriod + 2*time.Second)

		By("verifying metrics are gone after grace period")
		assertMetricsAbsent(metricsAddr, sliceMetrics.up)
	})

	It("should record 1 interruption when slice is deleted and recreated during grace period", func() {
		By("creating a Slice")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		By("updating the slice status to ready")
		updateSliceStatus(s, "SliceReady", metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)

		sliceMetrics := expectedMetricsForSliceWithState(s, "SliceReady")
		assertMetrics(metricsAddr, sliceMetrics.up.WithValue(1))

		By("deleting the slice")
		Expect(k8sClient.Delete(ctx, s)).To(Succeed())
		time.Sleep(2 * time.Second) // Less than grace period

		By("verifying interruption is recorded during grace period")
		assertMetrics(metricsAddr,
			sliceMetrics.up.WithValue(0),
			sliceMetrics.interruption_count.WithValue(1),
		)

		By("recreating the slice during grace period")
		s2 := &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.Name,
				Namespace: "default",
				Labels:    s.Labels,
			},
			Spec: s.Spec,
		}
		Expect(k8sClient.Create(ctx, s2)).To(Succeed())

		By("updating the new slice status to ready")
		updateSliceStatus(s2, "SliceReady", metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s2)).To(Succeed())
		time.Sleep(3 * time.Second)

		By("verifying recovery is recorded")
		assertMetrics(metricsAddr,
			sliceMetrics.up.WithValue(1),
			sliceMetrics.interruption_count.WithValue(1),
			sliceMetrics.recovery_count.WithValue(1),
		)

		By("checking for >0 for time between interruptions and recovery metrics")
		Eventually(func(g Gomega) {
			m, err := fetchMetrics(metricsAddr)
			g.Expect(err).NotTo(HaveOccurred())

			// Helper to check if metric value > 0
			checkGreaterThanZero := func(metricName string) {
				fullName := expectedMetricPrefix + "_" + metricName + "{"
				line := findMatchingLine(m, fullName)
				g.Expect(line).NotTo(BeEmpty(), "Metric %s not found in:\n%s", metricName, m)
				parts := strings.Fields(line)
				g.Expect(parts).To(HaveLen(2), "Unexpected metric line format: %s", line)
				val := parts[1]
				g.Expect(val).NotTo(Equal("0"), "Metric %s should be > 0, but got %s", metricName, val)
			}

			checkGreaterThanZero("slice_up_time_between_interruption_latest_seconds")
			checkGreaterThanZero("slice_down_time_between_recovery_latest_seconds")
			checkGreaterThanZero("slice_up_time_seconds")
			checkGreaterThanZero("slice_down_time_seconds")
		}, "10s", "1s").Should(Succeed())
	})
})

var _ = Describe("Slice State Transition Scenarios", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var metricsAddr string
	var s *slice.Slice
	var restCfg *rest.Config
	var k8sClient client.Client

	BeforeAll(func() {
		ctx, cancel = context.WithCancel(context.Background())
		_, restCfg, k8sClient = startTestEnv()
		DeferCleanup(func() {
			cancel()
			time.Sleep(3 * time.Second) // Wait for manager shutdown
		})
		metricsAddr = startManager(ctx, true, restCfg)
	})

	It("should handle complex state transitions", func() {
		s = &slice.Slice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice-transitions",
				Namespace: "default",
				Labels: map[string]string{
					"tpu-provisioner.cloud.google.com/owner-name":      "test-owner",
					"tpu-provisioner.cloud.google.com/owner-namespace": "default",
					"tpu-provisioner.cloud.google.com/owner-kind":      "test-kind",
				},
			},
			Spec: slice.SliceSpec{
				Type:         slice.TypeTpu7x,
				Topology:     "2x2",
				PartitionIds: []string{"p1"},
			},
		}

		By("creating a slice in ACTIVATING state")
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		By("updating slice status to ACTIVATING")
		updateSliceStatus(s, SLICE_STATE_ACTIVATING, metav1.ConditionFalse)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		// Allow time for aggregation
		time.Sleep(3 * time.Second)

		expectedSliceMetrics := expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVATING)
		assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(0),
			expectedSliceMetrics.interruption_count.WithValue(0))

		By("updating slice status to ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)
		expectedSliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVE)
		assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(1),
			expectedSliceMetrics.interruption_count.WithValue(0))

		By("updating slice status to ACTIVE_DEGRADED")
		updateSliceStatus(s, SLICE_STATE_ACTIVE_DEGRADED, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)
		expectedSliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVE_DEGRADED)
		assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(1),
			expectedSliceMetrics.interruption_count.WithValue(0),
			expectedSliceMetrics.recovery_count.WithValue(0))

		By("updating slice status to INCOMPLETE")
		updateSliceStatus(s, SLICE_STATE_INCOMPLETE, metav1.ConditionFalse)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)
		expectedSliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_INCOMPLETE)
		assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(0),
			expectedSliceMetrics.interruption_count.WithValue(1))

		By("updating slice status to ACTIVE")
		updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
		Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
		time.Sleep(3 * time.Second)
		expectedSliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVE)
		assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(1),
			expectedSliceMetrics.interruption_count.WithValue(1),
			expectedSliceMetrics.recovery_count.WithValue(1))

		// Test additional slice down states
		statesToTestDown := []string{"FAILED", "UNKNOWN", "HEALTH_STATUS_UNSPECIFIED"}
		for i, state := range statesToTestDown {
			By(fmt.Sprintf("updating slice status to %s", state))
			updateSliceStatus(s, state, metav1.ConditionFalse)
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
			time.Sleep(3 * time.Second)

			// slice metric should show down
			expectedSliceMetrics = expectedMetricsForSliceWithState(s, state)
			assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(0),
				expectedSliceMetrics.interruption_count.WithValue(2+i))

			By("updating slice status to ACTIVE")
			updateSliceStatus(s, SLICE_STATE_ACTIVE, metav1.ConditionTrue)
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())
			time.Sleep(3 * time.Second)
			expectedSliceMetrics = expectedMetricsForSliceWithState(s, SLICE_STATE_ACTIVE)
			assertMetrics(metricsAddr, expectedSliceMetrics.up.WithValue(1),
				expectedSliceMetrics.recovery_count.WithValue(2+i))
		}
	})
})

func expectedMetricsForSliceWithState(s *slice.Slice, state string) upnessMetrics {
	sLabels := map[string]interface{}{
		"slice_name":            s.Name,
		"slice_owner_name":      s.Labels["tpu-provisioner.cloud.google.com/owner-name"],
		"slice_owner_namespace": s.Labels["tpu-provisioner.cloud.google.com/owner-namespace"],
		"slice_owner_kind":      s.Labels["tpu-provisioner.cloud.google.com/owner-kind"],
		"tpu_accelerator":       string(s.Spec.Type),
		"tpu_topology":          s.Spec.Topology,
	}
	if state != "" {
		sLabels["slice_state"] = state
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
	}
}

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
	var metrics string
	Eventually(func() (string, error) {
		var err error
		metrics, err = fetchMetrics(addr)
		return metrics, err
	}, "3s", "1s").Should(ContainSubstring(expected[0].String()), "initial metric not found")
	for _, exp := range expected {
		Expect(metrics).To(ContainSubstring(exp.name+"{"), "metric name")
		line := findMatchingLine(metrics, exp.name+"{")
		//fmt.Println("-----------------------")
		//fmt.Println(metrics)
		Expect(metrics).To(ContainSubstring(exp.String()), "full metric does not match: "+line)
	}
}

func assertMetricsAbsent(addr string, expected ...metric) {
	GinkgoHelper()
	Consistently(func() (string, error) {
		metrics, err := fetchMetrics(addr)
		if err != nil {
			return "", err
		}
		return metrics, nil
	}, "3s", "1s").ShouldNot(ContainSubstring(expected[0].name+"{"), "metric found but should be absent")
}

func findMatchingLine(lines, match string) string {
	for _, line := range strings.Split(lines, "\n") {
		if strings.HasPrefix(line, match) {
			return line
		}
	}
	return ""
}

type metric struct {
	name   string
	labels map[string]interface{}
	value  interface{}
}

func (m metric) WithValue(val interface{}) metric {
	cp := m
	cp.value = val
	return cp
}

func (m metric) String() string {
	if m.value != nil {
		return m.valueString(m.value)
	}
	return m.valuelessString()
}

func (m metric) valueString(val interface{}) string {
	return fmt.Sprintf("%s %d", m.valuelessString(), val)
}

func (m metric) valuelessString() string {
	var labels = make(map[string]interface{}, len(m.labels))
	for k, v := range m.labels {
		labels[k] = v
	}
	labels["otel_scope_name"] = "megamon"
	labels["otel_scope_version"] = ""
	sortedKeys := make([]string, 0, len(labels))
	for k := range labels {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	str := expectedMetricPrefix + "_" + m.name + "{"
	for i, k := range sortedKeys {
		str += fmt.Sprintf("%s=\"%v\"", k, labels[k])
		if i < len(sortedKeys)-1 {
			str += ","
		}
	}
	str += "}"
	return str
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
		records.AppendUpEvent(t0, &rec, false, false)

		// 3. T+10m: Component becomes Ready (Up)
		t1 := t0.Add(10 * time.Minute)
		records.AppendUpEvent(t1, &rec, true, false)

		// 4. T+30m: Component goes into EXPECTED maintenance
		// This should NOT count as an interruption.
		t2 := t0.Add(30 * time.Minute)
		records.AppendUpEvent(t2, &rec, false, true) // isUp=false, expected=true

		// 5. T+60m: Component comes back Up
		t3 := t0.Add(60 * time.Minute)
		records.AppendUpEvent(t3, &rec, true, false)

		// 6. T+90m: Component crashes (UNPLANNED down)
		// This SHOULD count as an interruption.
		t4 := t0.Add(90 * time.Minute)
		records.AppendUpEvent(t4, &rec, false, false) // isUp=false, expected=false

		// 7. T+100m: Component recovers
		t5 := t0.Add(100 * time.Minute)
		records.AppendUpEvent(t5, &rec, true, false)

		// Verify at T+120m
		now := t0.Add(120 * time.Minute)
		summary := rec.Summarize(ctx, now)

		// 1. Interruption Count should be exactly 1 (the crash at T+90m).
		Expect(summary.InterruptionCount).To(Equal(1), "InterruptionCount mismatch")

		// 2. Recovery Count should be 2.
		Expect(summary.RecoveryCount).To(Equal(2), "RecoveryCount mismatch")

		// 3. Check Downtime Durations
		// Initial Down: t0 -> t1 = 10m
		Expect(summary.DownTimeInitial).To(Equal(10*time.Minute), "DownTimeInitial mismatch")

		// Total Down Time: 10m + 30m + 10m = 50m
		Expect(summary.DownTime).To(Equal(50*time.Minute), "Total DownTime mismatch")

		// Total Up Time: 20m + 30m + 20m = 70m
		Expect(summary.UpTime).To(Equal(70*time.Minute), "Total UpTime mismatch")
	})
})

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	containerv1beta1 "google.golang.org/api/container/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
			Name:      "js-rj-16",
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

var _ = Describe("Nodepool metrics", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

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
					"cloud.google.com/gke-nodepool": nodePoolName,
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
			nodepool := expectedMetricsForNodePool(np, jsRef.Name, jobRef.Name)
			assertMetrics(
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

	})
})

var _ = Describe("JobSet metrics", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		jsRef := types.NamespacedName{
			Name:      "test-js",
			Namespace: "default",
		}

		//BeforeEach(func() {
		//})

		//AfterEach(func() {
		//	By("Cleanup the specific resource instance")
		//	Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		//})

		var js *jobset.JobSet
		It("should watch a JobSet", func() {
			js = &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jsRef.Name,
					Namespace: jsRef.Namespace,
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "job1",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									BackoffLimit: ptr.To[int32](4),
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
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
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, js)).To(Succeed())
		})

		It("should publish metrics after submitting a jobset", func() {
			jobset := expectedMetricsForJobSet(js)
			assertMetrics(
				jobset.up.WithValue(0),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.interruption_count.WithValue(0),
				jobset.recovery_count.WithValue(0),
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
			jobset := expectedMetricsForJobSet(js)
			assertMetrics(
				jobset.up.WithValue(1),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.down_time_initial_seconds,
				jobset.interruption_count.WithValue(0),
				jobset.recovery_count.WithValue(0),
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
			jobset := expectedMetricsForJobSet(js)
			assertMetrics(
				jobset.up.WithValue(0),
				jobset.up_time_seconds,
				jobset.down_time_seconds,
				jobset.down_time_initial_seconds,
				jobset.up_time_between_interruption_seconds,
				jobset.up_time_between_interruption_mean_seconds,
				jobset.up_time_between_interruption_latest_seconds,
				jobset.interruption_count.WithValue(1),
				jobset.recovery_count.WithValue(0),
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
			jobset := expectedMetricsForJobSet(js)
			assertMetrics(
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
			)
		})

		It("should watch a jobset with a single replicated job", func() {
			Expect(k8sClient.Create(ctx, jobsetSingleJob)).To(Succeed())
		})
		It("should publish total TPU chip counts by jobset", func() {
			By("looking at TPU topology per replicated job in a deployed jobset")
			metrics := expectedMetricsForJobSet(jobsetSingleJob)
			metrics.tpu_chip_count.labels["tpu_topology"] = "2x4"
			assertMetrics(
				metrics.tpu_chip_count.WithValue(8),
			)
		})

		It("should watch a jobset with a two replicated jobs", func() {
			Expect(k8sClient.Create(ctx, jobsetMultipleRJobs)).To(Succeed())
		})
		It("should publish total TPU chip counts by jobset with multiple replicated jobs with >1 replica", func() {
			By("looking at TPU topology per replicated job in a deployed jobset")
			metrics := expectedMetricsForJobSet(jobsetMultipleRJobs)
			metrics.tpu_chip_count.labels["tpu_topology"] = "2x4"
			assertMetrics(
				metrics.tpu_chip_count.WithValue(24),
			)
		})
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

func expectedMetricsForNodePool(np *containerv1beta1.NodePool, jobSetName string, jobName string) utilizationMetrics {
	nodepoolLabels := map[string]interface{}{
		"nodepool_name": np.Name,
		"tpu_topology":  tpuTopology,
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

func expectedMetricsForJobSet(js *jobset.JobSet) upnessMetrics {
	// megamon_test_jobset_up{jobset_name="test-js",jobset_namespace="default",jobset_uid="a9876d7f-4639-41a3-9961-9ac68e0fcb7b",otel_scope_name="megamon",otel_scope_version=""} 0
	jsLabels := map[string]interface{}{
		"jobset_name":      js.Name,
		"jobset_namespace": js.Namespace,
		"jobset_uid":       js.UID,
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

func fetchMetrics() (string, error) {
	resp, err := http.Get("http://" + testCfg.MetricsAddr + "/metrics")
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

func assertMetrics(expected ...metric) {
	GinkgoHelper()
	var metrics string
	Eventually(func() (string, error) {
		var err error
		metrics, err = fetchMetrics()
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

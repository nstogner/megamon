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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

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

		It("should have the required ConfigMaps", func() {
			jsEventsCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCfg.JobSetEventsConfigMapRef.Name,
					Namespace: testCfg.JobSetEventsConfigMapRef.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, jsEventsCM)).To(Succeed())
			jsNodesCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCfg.JobSetNodeEventsConfigMapRef.Name,
					Namespace: testCfg.JobSetNodeEventsConfigMapRef.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, jsNodesCM)).To(Succeed())
			nodePoolsCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testCfg.NodePoolEventsConfigMapRef.Name,
					Namespace: testCfg.NodePoolEventsConfigMapRef.Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, nodePoolsCM)).To(Succeed())
		})

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
			assertMetrics(jobset.up.WithValue(0))
		})

		It("should publish updated metrics after jobset is marked as ready", func() {
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
			)
		})
	})
})

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
		Expect(metrics).To(ContainSubstring(exp.name), "metric name")
		Expect(metrics).To(ContainSubstring(exp.String()), "full metric")
	}
}

type upnessMetrics struct {
	up metric
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

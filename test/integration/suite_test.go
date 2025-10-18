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
	"os"
	"path/filepath"
	"strings"
	"testing"

	containerv1beta1 "google.golang.org/api/container/v1beta1"

	"example.com/megamon/internal/manager"
	"example.com/megamon/internal/records"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports
	"github.com/onsi/gomega/format"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var restCfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

var gkeClient = createStubGKEClient()

const (
	testMetricsPrefix = "megamon.test"
	nodePoolName      = "test-nodepool"
	tpuTopology       = "16x16"
)

var expectedMetricPrefix = strings.ReplaceAll(testCfg.MetricsPrefix, ".", "_")

var testCfg = manager.Config{
	MetricsPrefix:              testMetricsPrefix,
	AggregationIntervalSeconds: 1,
	EventsBucketName:           "test-bucket",
	EventsBucketPath:           "test-path",
	MetricsAddr:                "127.0.0.1:28080",
	ProbeAddr:                  "127.0.0.1:28081",
	UnknownCountThreshold:      0.1,
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	format.MaxLength = 30000 // Gomega default is 4000, anything past "MaxLength" will get truncateed in output

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	var err error
	// cfg is defined in this file globally.
	restCfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(restCfg).NotTo(BeNil())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	go func() {
		manager.MustRun(ctx, testCfg, restCfg,
			gkeClient,
			&mockGCSClient{records: map[string]map[string]records.EventRecords{}},
		)
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

type mockGKEClient struct {
	nodePools []*containerv1beta1.NodePool
}

func (m *mockGKEClient) ListNodePools(ctx context.Context) ([]*containerv1beta1.NodePool, error) {
	return m.nodePools, nil
}

func createStubNodePool() *containerv1beta1.NodePool {
	return &containerv1beta1.NodePool{
		Name: nodePoolName,
		Config: &containerv1beta1.NodeConfig{
			MachineType: "ct5lp-hightpu-4t",
			DiskSizeGb:  100,
			Labels: map[string]string{
				"cloud.google.com/gke-tpu-accelerator": "tpu-v5p",
			},
		},
		Autoscaling: &containerv1beta1.NodePoolAutoscaling{
			Enabled:      true,
			MinNodeCount: 1,
			MaxNodeCount: 3,
		},
		PlacementPolicy: &containerv1beta1.PlacementPolicy{
			TpuTopology: tpuTopology,
		},
	}
}

func createStubGKEClient() *mockGKEClient {
	return &mockGKEClient{
		nodePools: []*containerv1beta1.NodePool{
			createStubNodePool(),
		},
	}
}

type mockGCSClient struct {
	records map[string]map[string]records.EventRecords
}

func (m *mockGCSClient) GetRecords(ctx context.Context, bucket, path string) (map[string]records.EventRecords, error) {
	rec, ok := m.records[path]
	if !ok {
		return map[string]records.EventRecords{}, nil
	}
	return rec, nil
}

func (m *mockGCSClient) PutRecords(ctx context.Context, bucket, path string, recs map[string]records.EventRecords) error {
	m.records[path] = recs
	return nil
}

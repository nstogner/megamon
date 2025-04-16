package manager

import (
	"testing"

	"example.com/megamon/internal/experiments"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestLoadConfig(t *testing.T) {
	baseTestJSON := `
	{
  "MetricsPrefix": "megamon.alpha",
  "Exporters": [
    "stdout"
  ],
  "AggregationIntervalSeconds": 3,
  "GKE": {
    "ProjectID": "cool-machine-learning",
    "ClusterLocation": "us-east5",
    "ClusterName": "tpu-provisioner-e2e-us-east5"
  },
  "EventsBucketName": "cool-machine-learning-megamon",
  "DisableNodePoolJobLabelling": true
}`
	experimentJSON := `
		{
  "MetricsPrefix": "megamon.alpha",
  "Exporters": [
    "stdout"
  ],
  "AggregationIntervalSeconds": 3,
  "GKE": {
    "ProjectID": "cool-machine-learning",
    "ClusterLocation": "us-east5",
    "ClusterName": "tpu-provisioner-e2e-us-east5"
  },
  "EventsBucketName": "cool-machine-learning-megamon",
  "DisableNodePoolJobLabelling": true,
  "Experiments": {
    "NodeUnknownAsReady": {
      "Enabled": true
    },
	"ExperimentWithValueFloat" : {
		"Enabled": true,
		"Value": 8.5
	}
  }
}`
	baseConfig := Config{
		ReportConfigMapRef: types.NamespacedName{
			Namespace: "megamon-system",
			Name:      "megamon-report",
		},
		MetricsPrefix:              "megamon.alpha",
		MetricsAddr:                ":8080",
		ProbeAddr:                  ":8081",
		SecureMetrics:              true,
		EnableHTTP2:                false,
		Exporters:                  []string{"stdout"},
		AggregationIntervalSeconds: 3,
		GKE: GKEConfig{
			ProjectID:       "cool-machine-learning",
			ClusterLocation: "us-east5",
			ClusterName:     "tpu-provisioner-e2e-us-east5",
		},
		EventsBucketName:            "cool-machine-learning-megamon",
		DisableNodePoolJobLabelling: true,
		Experiments:                 map[string]experiments.ExperimentConfig{},
	}
	experimentConfig := baseConfig
	experimentConfig.Experiments = map[string]experiments.ExperimentConfig{
		"NodeUnknownAsReady": {
			Enabled: true,
		},
		"ExperimentWithValueFloat": {
			Enabled: true,
			Value:   float64(8.5),
		},
	}

	tests := []struct {
		name       string
		configJSON string
		want       Config
		wantErr    bool
	}{
		{
			name:       "base",
			configJSON: baseTestJSON,
			want:       baseConfig,
		},
		{
			name:       "experiments",
			configJSON: experimentJSON,
			want:       experimentConfig,
		},
	}
	for _, test := range tests {
		got, err := loadConfig([]byte(test.configJSON))
		if test.wantErr {
			if err == nil {
				t.Errorf("loadConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		}
		require.Equal(t, test.want, got)
	}

}

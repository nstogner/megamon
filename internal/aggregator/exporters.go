package aggregator

import (
	"context"
	"encoding/json"
	"os"

	"example.com/megamon/internal/records"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StdoutExporter struct{}

func (e *StdoutExporter) Export(_ context.Context, r records.Report) error {
	return json.NewEncoder(os.Stdout).Encode(r)
}

type ConfigMapExporter struct {
	Ref types.NamespacedName
	Key string
	client.Client
}

func (e *ConfigMapExporter) Export(ctx context.Context, r records.Report) error {
	cm := &corev1.ConfigMap{}
	if err := e.Get(ctx, e.Ref, cm); err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	jsn, err := json.Marshal(r)
	if err != nil {
		return err
	}
	cm.Data[e.Key] = string(jsn)
	if err := e.Update(ctx, cm); err != nil {
		return err
	}

	return nil
}

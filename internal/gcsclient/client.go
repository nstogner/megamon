package gcsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"example.com/megamon/internal/records"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("gcsclient")

type Client struct {
	StorageClient *storage.Client
}

func (c *Client) GetRecords(ctx context.Context, bucket, path string) (map[string]records.EventRecords, error) {
	rc, err := c.StorageClient.Bucket(bucket).Object(path).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return map[string]records.EventRecords{}, nil
		}
		return nil, fmt.Errorf("failed to read object %q: %v", path, err)
	}

	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read body: %v", err)
	}

	var recs map[string]records.EventRecords
	if err := json.Unmarshal([]byte(data), &recs); err != nil {
		return nil, err
	}
	log.V(3).Info("got records", "count", len(recs), "bucket", bucket, "path", path)
	return recs, nil
}

func (c *Client) PutRecords(ctx context.Context, bucket, path string, recs map[string]records.EventRecords) error {
	log.V(3).Info("putting records", "count", len(recs), "bucket", bucket, "path", path)
	data, err := json.Marshal(recs)
	if err != nil {
		return err
	}

	wc := c.StorageClient.Bucket(bucket).Object(path).NewWriter(ctx)
	if _, err := wc.Write(data); err != nil {
		return fmt.Errorf("failed to write object %q: %v", path, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("failed to close object %q: %v", path, err)
	}

	return nil
}

package gcsclient

import (
	"context"
	"sync"

	"example.com/megamon/internal/records"
)

type mockGCSClient struct {
	mu      sync.RWMutex
	records map[string]map[string]records.EventRecords
}

func (m *mockGCSClient) GetRecords(ctx context.Context, bucket, path string) (map[string]records.EventRecords, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.records[path]
	if !ok {
		return map[string]records.EventRecords{}, nil
	}
	return records.CloneEventRecordsMap(rec), nil
}

func (m *mockGCSClient) PutRecords(ctx context.Context, bucket, path string, recs map[string]records.EventRecords) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[path] = records.CloneEventRecordsMap(recs)
	return nil
}

func CreateStubGCSClient() *mockGCSClient {
	return &mockGCSClient{
		records: make(map[string]map[string]records.EventRecords),
	}
}

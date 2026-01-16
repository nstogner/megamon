package gcsclient

import (
	"context"

	"example.com/megamon/internal/records"
)

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

func CreateStubGCSClient() *mockGCSClient {
	return &mockGCSClient{records: map[string]map[string]records.EventRecords{}}
}

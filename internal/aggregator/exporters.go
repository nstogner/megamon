package aggregator

import (
	"encoding/json"
	"os"

	"example.com/megamon/internal/records"
)

type StdoutExporter struct{}

func (e *StdoutExporter) Export(r records.Report) error {
	return json.NewEncoder(os.Stdout).Encode(r)
}

package records

import "math"

func NewReport() Report {
	return Report{
		JobSetsUp:              make(map[string]Upness),
		JobSetsUpSummaries:     make(map[string]UpnessSummaryWithAttrs),
		JobSetNodesUp:          make(map[string]Upness),
		JobSetNodesUpSummaries: make(map[string]UpnessSummaryWithAttrs),
		NodePoolsUp:            make(map[string]Upness),
		NodePoolsUpSummaries:   make(map[string]UpnessSummaryWithAttrs),
		SlicesUp:               make(map[string]Upness),
		SlicesUpSummaries:      make(map[string]UpnessSummaryWithAttrs),
	}
}

type Report struct {
	JobSetsUp              map[string]Upness                 `json:"jobSetsUp"`
	JobSetsUpSummaries     map[string]UpnessSummaryWithAttrs `json:"jobSetsUpSummaries"`
	JobSetNodesUp          map[string]Upness                 `json:"jobSetNodesUp"`
	JobSetNodesUpSummaries map[string]UpnessSummaryWithAttrs `json:"jobSetNodesUpSummaries"`
	NodePoolsUp            map[string]Upness                 `json:"nodePoolsUp"`
	NodePoolsUpSummaries   map[string]UpnessSummaryWithAttrs `json:"nodePoolsUpSummaries"`
	SlicesUp               map[string]Upness                 `json:"slicesUp"`
	SlicesUpSummaries      map[string]UpnessSummaryWithAttrs `json:"slicesUpSummaries"`
	NodePoolScheduling     map[string]ScheduledJob           `json:"nodePoolScheduling"`
}

type Attrs struct {
	JobSetName      string `json:"jobsetName"`
	JobSetNamespace string `json:"jobsetNamespace"`
	JobSetUID       string `json:"jobsetUID"`

	TPUTopology    string `json:"tpuTopology"`
	TPUAccelerator string `json:"tpuAccelerator"`
	TPUChipCount   int32  `json:"tpuChipCount"`
	Spot           bool   `json:"spot"`

	NodePoolName string `json:"nodePoolName"`

	SliceName      string `json:"sliceName"`
	SliceOwnerName string `json:"sliceOwner"`
	SliceOwnerKind string `json:"sliceOwnerKind"`
}

type Upness struct {
	ReadyCount    int32 `json:"readyCount"`
	ExpectedCount int32 `json:"expectedCount"`
	UnknownCount  int32 `json:"unknownCount"`
	Attrs
}

// Up determines if a component is considered "up" based on its ready, expected, and unknown counts.
// It allows for a configurable threshold (unknownThreshold) of unknown instances to still be considered "up".
func (up Upness) Up(unknownThreshold float64) bool {
	maxUnknown := int32(math.RoundToEven(float64(up.ExpectedCount) * unknownThreshold))
	if up.UnknownCount > maxUnknown {
		return false
	}
	if up.ReadyCount+up.UnknownCount == up.ExpectedCount {
		return true
	}
	return false
}

type ScheduledJob struct {
	JobName    string `json:"jobName"`
	JobSetName string `json:"jobsetName"`
}

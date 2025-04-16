package records

import (
	"math"

	"example.com/megamon/internal/experiments"
)

func NewReport() Report {
	return Report{
		JobSetsUp:              make(map[string]Upness),
		JobSetsUpSummaries:     make(map[string]UpnessSummaryWithAttrs),
		JobSetNodesUp:          make(map[string]Upness),
		JobSetNodesUpSummaries: make(map[string]UpnessSummaryWithAttrs),
		NodePoolsUp:            make(map[string]Upness),
		NodePoolsUpSummaries:   make(map[string]UpnessSummaryWithAttrs),
	}
}

type Report struct {
	JobSetsUp              map[string]Upness                 `json:"jobSetsUp"`
	JobSetsUpSummaries     map[string]UpnessSummaryWithAttrs `json:"jobSetsUpSummaries"`
	JobSetNodesUp          map[string]Upness                 `json:"jobSetNodesUp"`
	JobSetNodesUpSummaries map[string]UpnessSummaryWithAttrs `json:"jobSetNodesUpSummaries"`
	NodePoolsUp            map[string]Upness                 `json:"nodePoolsUp"`
	NodePoolsUpSummaries   map[string]UpnessSummaryWithAttrs `json:"nodePoolsUpSummaries"`
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
}

type Upness struct {
	ReadyCount    int32 `json:"readyCount"`
	ExpectedCount int32 `json:"expectedCount"`
	UnknownCount  int32 `json:"unknownCount"`
	Attrs
}

func (up Upness) Up() bool {
	if experiments.IsExperimentEnabled("NodeUnknownAsNotReady") {
		val, err := experiments.GetExperimentValueFloat("NodeUnknownAsNotReady")
		if err != nil {
			log.Error(err, "failed to get NodeUnknownAsNotReady experiment value")
		}
		log.Info("NodeUnknownAsNotReady experiment enabled", "val", val, "ready", up.ReadyCount, "expected", up.ExpectedCount, "unknown", up.UnknownCount, "nodepool", up.NodePoolName, "jobset", up.JobSetName)
		// tolerate up to "NodeUnknownAsNotReady" value of Nodes being unknown (value expected between 0 and 1.0)
		if val < 0 || val > 1 {
			log.Info("invalid value for NodeUnknownAsNotReady experiment, ignoring")
			return up.ReadyCount == up.ExpectedCount
		}
		maxUnknown := int32(math.RoundToEven(float64(up.ExpectedCount) * val))
		if up.UnknownCount > maxUnknown {
			return false
		}
		if up.ReadyCount+up.UnknownCount == up.ExpectedCount {
			return true
		}
		return false
	}
	return up.ReadyCount == up.ExpectedCount
}

type ScheduledJob struct {
	JobName    string `json:"jobName"`
	JobSetName string `json:"jobsetName"`
}

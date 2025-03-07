package records

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
	Attrs
}

func (up Upness) Up() bool {
	return up.ReadyCount == up.ExpectedCount
}

type ScheduledJob struct {
	JobName    string `json:"jobName"`
	JobSetName string `json:"jobsetName"`
}

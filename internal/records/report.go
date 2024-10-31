package records

func NewReport() Report {
	return Report{
		JobSetsUp:              make(map[string]Upness),
		JobSetsUpSummaries:     make(map[string]EventSummaryWithAttrs),
		JobSetNodesUp:          make(map[string]Upness),
		JobSetNodesUpSummaries: make(map[string]EventSummaryWithAttrs),
	}
}

type Report struct {
	JobSetsUp              map[string]Upness                `json:"jobSetsUp"`
	JobSetsUpSummaries     map[string]EventSummaryWithAttrs `json:"jobSetsUpSummaries"`
	JobSetNodesUp          map[string]Upness                `json:"jobSetNodesUp"`
	JobSetNodesUpSummaries map[string]EventSummaryWithAttrs `json:"jobSetNodesUpSummaries"`
}

type Attrs struct {
	JobSetName      string `json:"jobsetName"`
	JobSetNamespace string `json:"jobsetNamespace"`

	TPUTopology    string `json:"tpuTopology"`
	TPUAccelerator string `json:"tpuAccelerator"`
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

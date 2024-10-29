package records

func NewReport() Report {
	return Report{
		JobSetsUp:          make(map[string]JobSetUp),
		JobSetNodesUp:      make(map[string]JobSetNodesUp),
		JobSetsUpSummaries: make(map[string]EventSummaryWithAttrs),
	}
}

type Report struct {
	JobSetsUp          map[string]JobSetUp              `json:"jobSetsUp"`
	JobSetsUpSummaries map[string]EventSummaryWithAttrs `json:"jobSetsUpSummaries"`
	JobSetNodesUp      map[string]JobSetNodesUp         `json:"jobSetNodesUp"`
}

type JobSetUp struct {
	Up bool `json:"up"`
	JobSetAttrs
}

type JobSetAttrs struct {
	TPUTopology    string `json:"tpuTopology"`
	TPUAccelerator string `json:"tpuAccelerator"`
	Spot           bool   `json:"spot"`
}

type JobSetNodesUp struct {
	ReadyCount    int `json:"readyCount"`
	ExpectedCount int `json:"expectedCount"`
	JobSetAttrs
}

func (up JobSetNodesUp) Up() bool {
	return up.ReadyCount == up.ExpectedCount
}

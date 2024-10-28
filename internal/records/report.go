package records

func NewReport() Report {
	return Report{
		JobSetsUp:          make(map[string]JobSetUp),
		JobSetNodesUp:      make(map[string]JobSetNodesUp),
		JobSetsUpSummaries: make(map[string]JobSetUpSummaryWithAttrs),
	}
}

type Report struct {
	JobSetsUp          map[string]JobSetUp
	JobSetsUpSummaries map[string]JobSetUpSummaryWithAttrs
	JobSetNodesUp      map[string]JobSetNodesUp
}

type JobSetUp struct {
	Up bool
	JobSetAttrs
}

type JobSetAttrs struct {
	TPUTopology    string
	TPUAccelerator string
	Spot           bool
}

type JobSetNodesUp struct {
	ReadyCount    int
	ExpectedCount int
	JobSetAttrs
}

func (up JobSetNodesUp) Up() bool {
	return up.ReadyCount == up.ExpectedCount
}

package experiments

type ExperimentConfig struct {
	Name    string
	Enabled bool
}

var exp = []ExperimentConfig{}

func SetExperimentsConfig(c []ExperimentConfig) {
	exp = c
}

func IsExperimentEnabled(name string) bool {
	for _, e := range exp {
		if e.Name == name {
			return e.Enabled
		}
	}
	return false
}

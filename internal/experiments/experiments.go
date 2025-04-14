package experiments

import "fmt"

type ExperimentConfig struct {
	Enabled bool
	Value   interface{}
}

var exp = map[string]ExperimentConfig{}

func SetExperimentsConfig(c map[string]ExperimentConfig) {
	exp = c
}

func IsExperimentEnabled(name string) bool {
	if c, ok := exp[name]; ok {
		return c.Enabled
	}
	return false
}

func GetExperimentValueInt(name string) (int, error) {
	if c, ok := exp[name]; ok {
		return c.Value.(int), nil
	}
	return 0, fmt.Errorf("experiment %s not found", name)
}

func GetExperimentValueFloat(name string) (float64, error) {
	if c, ok := exp[name]; ok {
		return c.Value.(float64), nil
	}
	return 0, fmt.Errorf("experiment %s not found", name)
}

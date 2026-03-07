package devport

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServiceSpec struct {
	Key     string   `yaml:"key"`
	Exec    string   `yaml:"exec"`
	NoPort  bool     `yaml:"no-port"`
	Tailnet bool     `yaml:"tailnet"`
	PortEnv string   `yaml:"port-env"`
	Cwd     string   `yaml:"cwd"`
	Env     EnvPaths `yaml:"env"`
}

// EnvPaths handles both single string and list of strings in YAML.
type EnvPaths []string

func (e *EnvPaths) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*e = []string{value.Value}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*e = list
		return nil
	default:
		return fmt.Errorf("env: expected string or list, got %v", value.Kind)
	}
}

// ParseConfig reads a devport.yaml file and returns service specs.
func ParseConfig(path string) ([]ServiceSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw []yaml.Node
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	specs := make([]ServiceSpec, 0, len(raw))
	for i, node := range raw {
		switch node.Kind {
		case yaml.ScalarNode:
			// String shorthand: just a command
			specs = append(specs, ServiceSpec{Exec: node.Value})
		case yaml.MappingNode:
			var spec ServiceSpec
			if err := node.Decode(&spec); err != nil {
				return nil, fmt.Errorf("service %d: %w", i, err)
			}
			if spec.Exec == "" {
				return nil, fmt.Errorf("service %d: exec is required", i)
			}
			specs = append(specs, spec)
		default:
			return nil, fmt.Errorf("service %d: expected string or object", i)
		}
	}

	return specs, nil
}

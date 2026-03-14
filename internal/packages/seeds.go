package packages

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Seeds struct {
	PopularPlugins []string `yaml:"popular_plugins"`
	PopularThemes  []string `yaml:"popular_themes"`
}

func LoadSeeds(path string) (*Seeds, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading seeds file: %w", err)
	}

	var seeds Seeds
	if err := yaml.Unmarshal(data, &seeds); err != nil {
		return nil, fmt.Errorf("parsing seeds file: %w", err)
	}
	return &seeds, nil
}

package helpers

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type CapabilityMatrix struct {
	Lanes []CapabilityLane `yaml:"lanes"`
}

type CapabilityLane struct {
	ID                string   `yaml:"id"`
	Workstream        string   `yaml:"workstream"`
	Capability        string   `yaml:"capability"`
	CapabilityFamily  string   `yaml:"capabilityFamily"`
	EnvironmentClass  string   `yaml:"environmentClass"`
	AuthPattern       string   `yaml:"authPattern"`
	GoTestPattern     string   `yaml:"goTestPattern"`
	ExpectedArtifacts []string `yaml:"expectedArtifacts"`
}

type LiveProofMatrix struct {
	Lanes []LiveProofLane `yaml:"lanes"`
}

type LiveProofLane struct {
	ID                string   `yaml:"id"`
	Workstream        string   `yaml:"workstream"`
	Capability        string   `yaml:"capability"`
	EnvironmentClass  string   `yaml:"environmentClass"`
	AuthPattern       string   `yaml:"authPattern"`
	EvidenceChecklist string   `yaml:"evidenceChecklist"`
	Owner             string   `yaml:"owner"`
	Prerequisites     []string `yaml:"prerequisites"`
}

type WebsiteReviewRubric struct {
	Sections []WebsiteReviewSection `yaml:"sections"`
}

type WebsiteReviewSection struct {
	ID       string                  `yaml:"id"`
	Title    string                  `yaml:"title"`
	Criteria []WebsiteReviewCriteria `yaml:"criteria"`
}

type WebsiteReviewCriteria struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

func LoadCapabilityMatrix(path string) (*CapabilityMatrix, error) {
	var matrix CapabilityMatrix
	if err := loadYAMLFile(path, &matrix); err != nil {
		return nil, err
	}
	if len(matrix.Lanes) == 0 {
		return nil, fmt.Errorf("capability matrix has no lanes")
	}
	for i, lane := range matrix.Lanes {
		if strings.TrimSpace(lane.ID) == "" {
			return nil, fmt.Errorf("capability matrix lane %d missing id", i)
		}
		if strings.TrimSpace(lane.Workstream) == "" {
			return nil, fmt.Errorf("capability matrix lane %s missing workstream", lane.ID)
		}
		if strings.TrimSpace(lane.Capability) == "" {
			return nil, fmt.Errorf("capability matrix lane %s missing capability", lane.ID)
		}
		if strings.TrimSpace(lane.CapabilityFamily) == "" {
			return nil, fmt.Errorf("capability matrix lane %s missing capabilityFamily", lane.ID)
		}
		if strings.TrimSpace(lane.EnvironmentClass) == "" {
			return nil, fmt.Errorf("capability matrix lane %s missing environmentClass", lane.ID)
		}
		if strings.TrimSpace(lane.AuthPattern) == "" {
			return nil, fmt.Errorf("capability matrix lane %s missing authPattern", lane.ID)
		}
		if len(lane.ExpectedArtifacts) == 0 {
			return nil, fmt.Errorf("capability matrix lane %s missing expectedArtifacts", lane.ID)
		}
	}
	return &matrix, nil
}

func LoadLiveProofMatrix(path string) (*LiveProofMatrix, error) {
	var matrix LiveProofMatrix
	if err := loadYAMLFile(path, &matrix); err != nil {
		return nil, err
	}
	if len(matrix.Lanes) == 0 {
		return nil, fmt.Errorf("live proof matrix has no lanes")
	}
	for i, lane := range matrix.Lanes {
		if strings.TrimSpace(lane.ID) == "" {
			return nil, fmt.Errorf("live proof lane %d missing id", i)
		}
		if strings.TrimSpace(lane.EvidenceChecklist) == "" {
			return nil, fmt.Errorf("live proof lane %s missing evidenceChecklist", lane.ID)
		}
	}
	return &matrix, nil
}

func LoadWebsiteReviewRubric(path string) (*WebsiteReviewRubric, error) {
	var rubric WebsiteReviewRubric
	if err := loadYAMLFile(path, &rubric); err != nil {
		return nil, err
	}
	if len(rubric.Sections) == 0 {
		return nil, fmt.Errorf("website review rubric has no sections")
	}
	for i, section := range rubric.Sections {
		if strings.TrimSpace(section.ID) == "" {
			return nil, fmt.Errorf("website review section %d missing id", i)
		}
		if len(section.Criteria) == 0 {
			return nil, fmt.Errorf("website review section %s has no criteria", section.ID)
		}
	}
	return &rubric, nil
}

func (r *WebsiteReviewRubric) HasSection(id string) bool {
	for _, section := range r.Sections {
		if section.ID == id {
			return true
		}
	}
	return false
}

func loadYAMLFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

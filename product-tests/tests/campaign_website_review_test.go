package tests_test

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
	"gopkg.in/yaml.v3"
)

type websiteReviewRubric struct {
	Sections []websiteReviewSection `yaml:"sections"`
}

type websiteReviewSection struct {
	ID       string                  `yaml:"id"`
	Title    string                  `yaml:"title"`
	Criteria []websiteReviewCriteria `yaml:"criteria"`
}

type websiteReviewCriteria struct {
	ID          string   `yaml:"id"`
	Description string   `yaml:"description"`
	Path        string   `yaml:"path"`
	ExpectLinks []string `yaml:"expectLinks"`
	ExpectText  []string `yaml:"expectText"`
}

func TestCampaignWebsiteReview(t *testing.T) {
	fr := helpers.NewFindingsRecorder("website-review")
	fr.SetLaneMetadata("website-review", "website-information-architecture", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	repoRoot := websiteReviewRepoRoot(t)
	rubricPath := filepath.Join(repoRoot, "product-tests", "testdata", "fleet", "website-review-rubric.yaml")
	rubric := loadWebsiteReviewRubric(t, rubricPath)

	type checkedCriterion struct {
		ID          string   `json:"id"`
		Path        string   `json:"path"`
		ExpectLinks []string `json:"expectLinks,omitempty"`
		ExpectText  []string `json:"expectText,omitempty"`
	}
	checked := []checkedCriterion{}

	for _, section := range rubric.Sections {
		fr.CheckBool(
			"section-"+section.ID,
			"rubric section has at least one criterion: "+section.Title,
			len(section.Criteria) > 0,
			section.ID,
		)
		for _, criterion := range section.Criteria {
			checked = append(checked, checkedCriterion{
				ID:          criterion.ID,
				Path:        criterion.Path,
				ExpectLinks: criterion.ExpectLinks,
				ExpectText:  criterion.ExpectText,
			})

			hasBoundedChecks := criterion.Path != "" && (len(criterion.ExpectLinks) > 0 || len(criterion.ExpectText) > 0)
			fr.CheckBool(
				"criterion-"+criterion.ID+"-bounded",
				"criterion defines a bounded page check",
				hasBoundedChecks,
				criterion.Description,
			)
			if !hasBoundedChecks {
				continue
			}

			pagePath := filepath.Join(repoRoot, criterion.Path)
			content, err := os.ReadFile(pagePath)
			if err != nil {
				fr.Check(
					criterion.ID+"-page-readable",
					"criterion target page is readable",
					"readable file",
					err.Error(),
					false,
					pagePath,
				)
				continue
			}

			text := string(content)
			for _, link := range criterion.ExpectLinks {
				fr.Check(
					criterion.ID+"-link-"+sanitizeCriterionID(link),
					criterion.Description,
					link,
					linkPresence(text, link),
					contains(text, link),
					criterion.Path,
				)
			}
			for _, expected := range criterion.ExpectText {
				fr.Check(
					criterion.ID+"-text-"+sanitizeCriterionID(expected),
					criterion.Description,
					expected,
					linkPresence(text, expected),
					contains(text, expected),
					criterion.Path,
				)
			}
		}
	}

	if err := fr.AddJSONArtifact("website-review-checked-pages.json", map[string]any{
		"rubricPath": rubricPath,
		"checked":    checked,
	}); err != nil {
		t.Fatalf("record website review artifact: %v", err)
	}
}

func websiteReviewRepoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	testsDir := filepath.Dir(filename)
	return filepath.Dir(filepath.Dir(testsDir))
}

func loadWebsiteReviewRubric(t *testing.T, path string) websiteReviewRubric {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read website review rubric: %v", err)
	}
	var rubric websiteReviewRubric
	if err := yaml.Unmarshal(data, &rubric); err != nil {
		t.Fatalf("decode website review rubric: %v", err)
	}
	return rubric
}

func contains(text, expected string) bool {
	return strings.TrimSpace(expected) != "" && strings.Contains(text, expected)
}

func linkPresence(text, expected string) string {
	if contains(text, expected) {
		return expected
	}
	return "missing"
}

func sanitizeCriterionID(value string) string {
	clean := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			clean = append(clean, r)
		case r >= 'A' && r <= 'Z':
			clean = append(clean, r+'a'-'A')
		case r >= '0' && r <= '9':
			clean = append(clean, r)
		default:
			clean = append(clean, '-')
		}
	}
	return string(clean)
}

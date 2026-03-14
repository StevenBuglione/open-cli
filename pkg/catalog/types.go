package catalog

import (
	"net/http"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/obs"
)

type BuildOptions struct {
	Config       any
	BaseDir      string
	HTTPClient   *http.Client
	CacheDir     string
	ForceRefresh bool
	Observer     obs.Observer
}

type NormalizedCatalog struct {
	CatalogVersion    string          `json:"catalogVersion"`
	GeneratedAt       time.Time       `json:"generatedAt"`
	SourceFingerprint string          `json:"sourceFingerprint"`
	Sources           []SourceRecord  `json:"sources"`
	Services          []Service       `json:"services"`
	Tools             []Tool          `json:"tools"`
	Workflows         []Workflow      `json:"workflows,omitempty"`
	EffectiveViews    []EffectiveView `json:"effectiveViews"`
}

type SourceRecord struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	URI        string           `json:"uri"`
	Provenance SourceProvenance `json:"provenance"`
}

type SourceProvenance struct {
	Method  string        `json:"method"`
	At      time.Time     `json:"at"`
	Fetches []SourceFetch `json:"fetches,omitempty"`
}

type SourceFetch struct {
	URL           string     `json:"url"`
	FetchedAt     time.Time  `json:"fetchedAt"`
	Method        string     `json:"method"`
	RequestMethod string     `json:"requestMethod,omitempty"`
	StatusCode    int        `json:"statusCode,omitempty"`
	CacheOutcome  string     `json:"cacheOutcome,omitempty"`
	ETag          string     `json:"etag,omitempty"`
	LastModified  string     `json:"lastModified,omitempty"`
	CacheControl  string     `json:"cacheControl,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	Stale         bool       `json:"stale,omitempty"`
}

type Service struct {
	ID       string   `json:"id"`
	Alias    string   `json:"alias"`
	SourceID string   `json:"sourceId"`
	Title    string   `json:"title"`
	Servers  []string `json:"servers,omitempty"`
}

type Parameter struct {
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	Location     string `json:"location"`
	Required     bool   `json:"required"`
}

type Safety struct {
	Destructive      bool `json:"destructive"`
	ReadOnly         bool `json:"readOnly"`
	RequiresApproval bool `json:"requiresApproval"`
	Idempotent       bool `json:"idempotent"`
}

type GuidanceExample struct {
	Goal    string `json:"goal"`
	Command string `json:"command"`
}

type Guidance struct {
	WhenToUse []string          `json:"whenToUse,omitempty"`
	AvoidWhen []string          `json:"avoidWhen,omitempty"`
	Examples  []GuidanceExample `json:"examples,omitempty"`
}

type RequestBodyContent struct {
	MediaType string         `json:"mediaType"`
	Schema    map[string]any `json:"schema,omitempty"`
}

type RequestBody struct {
	Required     bool                 `json:"required"`
	ContentTypes []RequestBodyContent `json:"contentTypes,omitempty"`
}

type OutputHints struct {
	DefaultFields []string `json:"defaultFields,omitempty"`
	Redactions    []string `json:"redactions,omitempty"`
}

type PaginationHints struct {
	Style       string `json:"style,omitempty"`
	CursorParam string `json:"cursorParam,omitempty"`
}

type RetryHints struct {
	Recommended    bool `json:"recommended,omitempty"`
	LocationHeader bool `json:"locationHeader,omitempty"`
}

type Tool struct {
	ID          string            `json:"id"`
	ServiceID   string            `json:"serviceId"`
	OperationID string            `json:"operationId,omitempty"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Group       string            `json:"group"`
	Command     string            `json:"command"`
	Aliases     []string          `json:"aliases,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Hidden      bool              `json:"hidden,omitempty"`
	PathParams  []Parameter       `json:"pathParams,omitempty"`
	Flags       []Parameter       `json:"flags,omitempty"`
	RequestBody *RequestBody      `json:"requestBody,omitempty"`
	Auth        []AuthRequirement `json:"auth,omitempty"`
	Safety      Safety            `json:"safety"`
	Output      *OutputHints      `json:"output,omitempty"`
	Pagination  *PaginationHints  `json:"pagination,omitempty"`
	Retry       *RetryHints       `json:"retry,omitempty"`
	Guidance    *Guidance         `json:"guidance,omitempty"`
	Servers     []string          `json:"servers,omitempty"`
}

type AuthRequirement struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scheme    string `json:"scheme,omitempty"`
	In        string `json:"in,omitempty"`
	ParamName string `json:"paramName,omitempty"`
}

type Workflow struct {
	WorkflowID string         `json:"workflowId"`
	Steps      []WorkflowStep `json:"steps"`
}

type WorkflowStep struct {
	StepID string `json:"stepId"`
	ToolID string `json:"toolId"`
}

type EffectiveView struct {
	Name  string `json:"name"`
	Mode  string `json:"mode"`
	Tools []Tool `json:"tools"`
}

func (catalog *NormalizedCatalog) FindTool(id string) *Tool {
	for idx := range catalog.Tools {
		if catalog.Tools[idx].ID == id {
			return &catalog.Tools[idx]
		}
	}
	return nil
}

func (catalog *NormalizedCatalog) EffectiveView(name string) *EffectiveView {
	for idx := range catalog.EffectiveViews {
		if catalog.EffectiveViews[idx].Name == name {
			return &catalog.EffectiveViews[idx]
		}
	}
	return nil
}

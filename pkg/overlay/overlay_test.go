package overlay_test

import (
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/overlay"
)

func TestApplyUpdatesCopiesAndRemovesNodes(t *testing.T) {
	base := map[string]any{
		"paths": map[string]any{
			"/tickets": map[string]any{
				"get": map[string]any{
					"parameters": []any{
						map[string]any{"name": "status", "in": "query"},
						map[string]any{"name": "debug", "in": "query"},
					},
				},
			},
		},
	}

	doc := overlay.Document{
		Actions: []overlay.Action{
			{
				Target: "$.paths['/tickets'].get",
				Update: map[string]any{
					"x-cli-name": "list",
					"x-cli-safety": map[string]any{
						"readOnly": true,
					},
				},
			},
			{
				Target: "$.paths['/tickets'].get.parameters[?(@.name=='status')]",
				Update: map[string]any{
					"x-cli-name": "state",
				},
			},
			{
				Target: "$.paths['/tickets'].get.parameters[?(@.name=='status')]",
				Copy: &overlay.Copy{
					To: "$.paths['/tickets'].get.x-status-parameter",
				},
			},
			{
				Target: "$.paths['/tickets'].get.parameters[?(@.name=='debug')]",
				Remove: true,
			},
		},
	}

	got, err := overlay.Apply(base, doc)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	get := got["paths"].(map[string]any)["/tickets"].(map[string]any)["get"].(map[string]any)
	if get["x-cli-name"] != "list" {
		t.Fatalf("expected x-cli-name list, got %#v", get["x-cli-name"])
	}
	params := get["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("expected debug param to be removed, got %#v", params)
	}
	status := params[0].(map[string]any)
	if status["x-cli-name"] != "state" {
		t.Fatalf("expected status param rename, got %#v", status)
	}
	if copied := get["x-status-parameter"].(map[string]any); copied["name"] != "status" {
		t.Fatalf("expected copied parameter, got %#v", copied)
	}
}

func TestApplyRejectsInvalidJSONPath(t *testing.T) {
	_, err := overlay.Apply(map[string]any{}, overlay.Document{
		Actions: []overlay.Action{{Target: "$.paths[", Update: map[string]any{"x": "y"}}},
	})
	if err == nil {
		t.Fatalf("expected invalid jsonpath error")
	}
}

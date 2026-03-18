package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
)

func TestBuildSupportsServiceRootAndAPICatalogSources(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\", <%s>; rel=\"service-meta\"", server.URL+"/openapi.json", server.URL+"/metadata.json"))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/metadata.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"linkset": []map[string]any{
				{"href": server.URL + "/skills.json", "rel": "https://open-cli.dev/rel/skill-manifest"},
			},
		})
	})
	mux.HandleFunc("/skills.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"toolGuidance": map[string]any{
				"tickets:listTickets": map[string]any{
					"whenToUse": []string{"Need the latest tickets"},
				},
			},
		})
	})
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "openapi": "3.1.0",
		  "info": { "title": "Tickets API", "version": "1.0.0" },
		  "servers": [{ "url": "` + server.URL + `" }],
		  "paths": {
		    "/tickets": {
		      "get": {
		        "operationId": "listTickets",
		        "tags": ["tickets"],
		        "responses": {
		          "200": { "description": "OK" }
		        }
		      }
		    }
		  }
		}`))
	})
	mux.HandleFunc("/.well-known/api-catalog", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"linkset": []map[string]any{
				{"href": server.URL + "/service", "rel": "item"},
			},
		})
	})

	serviceRootConfig := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsService": {
				Type:    "serviceRoot",
				URI:     server.URL + "/service",
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source: "ticketsService",
				Alias:  "tickets",
			},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{Config: serviceRootConfig})
	if err != nil {
		t.Fatalf("Build(serviceRoot) returned error: %v", err)
	}
	if ntc.FindTool("tickets:listTickets") == nil {
		t.Fatalf("expected listTickets tool from serviceRoot source")
	}
	if ntc.FindTool("tickets:listTickets").Guidance == nil {
		t.Fatalf("expected skill guidance from service metadata")
	}

	apiCatalogConfig := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"publisher": {
				Type:    "apiCatalog",
				URI:     server.URL + "/.well-known/api-catalog",
				Enabled: true,
			},
		},
	}

	ntc, err = catalog.Build(context.Background(), catalog.BuildOptions{Config: apiCatalogConfig})
	if err != nil {
		t.Fatalf("Build(apiCatalog) returned error: %v", err)
	}
	if len(ntc.Services) != 1 || ntc.FindTool("tickets:listTickets") == nil {
		t.Fatalf("expected discovered service from apiCatalog source, got %#v %#v", ntc.Services, ntc.Tools)
	}
}

func TestBuildReusesCachedRemoteOpenAPIAndMarksStaleFallback(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	cacheDir := t.TempDir()

	mux.HandleFunc("/tickets.openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"tickets-openapi-v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(`{
		  "openapi": "3.1.0",
		  "info": { "title": "Tickets API", "version": "1.0.0" },
		  "servers": [{ "url": "https://api.example.com" }],
		  "paths": {
		    "/tickets": {
		      "get": {
		        "operationId": "listTickets",
		        "tags": ["tickets"],
		        "responses": { "200": { "description": "OK" } }
		      }
		    }
		  }
		}`))
	})

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsSource": {
				Type:    "openapi",
				URI:     server.URL + "/tickets.openapi.json",
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source: "ticketsSource",
				Alias:  "tickets",
			},
		},
	}

	first, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:     cfg,
		CacheDir:   cacheDir,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("first Build returned error: %v", err)
	}
	if len(first.Sources) != 1 || len(first.Sources[0].Provenance.Fetches) == 0 {
		t.Fatalf("expected source fetch provenance, got %#v", first.Sources)
	}
	if first.Sources[0].Provenance.Fetches[0].CacheOutcome != "miss" {
		t.Fatalf("expected first build cache miss, got %#v", first.Sources[0].Provenance.Fetches[0])
	}

	server.Close()

	second, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:     cfg,
		CacheDir:   cacheDir,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}
	if second.FindTool("tickets:listTickets") == nil {
		t.Fatalf("expected cached catalog build to retain tool")
	}
	if second.Sources[0].Provenance.Fetches[0].CacheOutcome != "stale_hit" {
		t.Fatalf("expected stale fallback provenance, got %#v", second.Sources[0].Provenance.Fetches[0])
	}
	if !second.Sources[0].Provenance.Fetches[0].Stale {
		t.Fatalf("expected stale marker in provenance, got %#v", second.Sources[0].Provenance.Fetches[0])
	}
}

func TestBuildResolvesRelativeMetadataReferences(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\", <%s>; rel=\"service-meta\"", server.URL+"/openapi.json", server.URL+"/metadata/linkset.json"))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/metadata/linkset.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"linkset": []map[string]any{
				{"href": "../skills/tickets.json", "rel": "https://open-cli.dev/rel/skill-manifest"},
			},
		})
	})
	mux.HandleFunc("/skills/tickets.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"toolGuidance": map[string]any{
				"tickets:listTickets": map[string]any{
					"whenToUse": []string{"Need cached guidance"},
				},
			},
		})
	})
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "openapi": "3.1.0",
		  "info": { "title": "Tickets API", "version": "1.0.0" },
		  "servers": [{ "url": "` + server.URL + `" }],
		  "paths": {
		    "/tickets": {
		      "get": {
		        "operationId": "listTickets",
		        "tags": ["tickets"],
		        "responses": { "200": { "description": "OK" } }
		      }
		    }
		  }
		}`))
	})

	cfg := config.Config{
		CLI:  "1.0.0",
		Mode: config.ModeConfig{Default: "discover"},
		Sources: map[string]config.Source{
			"ticketsService": {
				Type:    "serviceRoot",
				URI:     server.URL + "/service",
				Enabled: true,
			},
		},
		Services: map[string]config.Service{
			"tickets": {
				Source: "ticketsService",
				Alias:  "tickets",
			},
		},
	}

	ntc, err := catalog.Build(context.Background(), catalog.BuildOptions{
		Config:     cfg,
		HTTPClient: server.Client(),
		CacheDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ntc.FindTool("tickets:listTickets") == nil || ntc.FindTool("tickets:listTickets").Guidance == nil {
		t.Fatalf("expected relative metadata skill reference to resolve, got %#v", ntc.Tools)
	}
}

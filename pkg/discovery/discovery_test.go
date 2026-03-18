package discovery_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/cache"
	"github.com/StevenBuglione/open-cli/pkg/discovery"
)

func TestDiscoverAPICatalogFollowsNestedCatalogsAndReportsCycles(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/api-catalog", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
		  "linkset": [
		    { "href": %q, "rel": "item" },
		    { "href": %q, "rel": "api-catalog" }
		  ]
		}`, server.URL+"/services/tickets", server.URL+"/nested-catalog")
	})
	mux.HandleFunc("/nested-catalog", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
		  "linkset": [
		    { "href": %q, "rel": "item" },
		    { "href": %q, "rel": "api-catalog" }
		  ]
		}`, server.URL+"/services/billing", server.URL+"/.well-known/api-catalog")
	})

	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	result, err := discovery.DiscoverAPICatalog(context.Background(), cache.NewFetcher(cache.FetcherOptions{
		Store:  store,
		Client: server.Client(),
	}), server.URL+"/.well-known/api-catalog", cache.Policy{})
	if err != nil {
		t.Fatalf("DiscoverAPICatalog returned error: %v", err)
	}

	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %#v", result.Services)
	}
	if len(result.Provenance.Fetches) != 2 {
		t.Fatalf("expected 2 catalog fetches, got %#v", result.Provenance.Fetches)
	}
	if len(result.Warnings) == 0 || result.Warnings[0].Code != "api_catalog_cycle" {
		t.Fatalf("expected cycle warning, got %#v", result.Warnings)
	}
	if result.Provenance.Fetches[0].CacheOutcome != "miss" {
		t.Fatalf("expected cache miss provenance, got %#v", result.Provenance.Fetches[0])
	}
}

func TestDiscoverServiceRootUsesHeadThenFallbackToGet(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/service", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Link",
			fmt.Sprintf("<%s>; rel=\"service-desc\", <%s>; rel=\"service-meta\"",
				server.URL+"/openapi.json",
				server.URL+"/metadata.json",
			),
		)
		w.WriteHeader(http.StatusOK)
	})

	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	result, err := discovery.DiscoverServiceRoot(context.Background(), cache.NewFetcher(cache.FetcherOptions{
		Store:  store,
		Client: server.Client(),
	}), server.URL+"/service", cache.Policy{})
	if err != nil {
		t.Fatalf("DiscoverServiceRoot returned error: %v", err)
	}

	if result.OpenAPIURL != server.URL+"/openapi.json" {
		t.Fatalf("expected openapi url, got %q", result.OpenAPIURL)
	}
	if result.MetadataURL != server.URL+"/metadata.json" {
		t.Fatalf("expected metadata url, got %q", result.MetadataURL)
	}
	if result.Provenance.Method != discovery.ProvenanceRFC8631 {
		t.Fatalf("expected RFC8631 provenance, got %q", result.Provenance.Method)
	}
}

func TestDiscoverServiceRootRevalidatesHeadResponses(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	var sawConditional bool
	mux.HandleFunc("/service", func(w http.ResponseWriter, r *http.Request) {
		if match := r.Header.Get("If-None-Match"); match != "" {
			sawConditional = true
			if match != `"service-v1"` {
				t.Fatalf("expected If-None-Match \"service-v1\", got %q", match)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("ETag", `"service-v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		w.Header().Set("Link",
			fmt.Sprintf("<%s>; rel=\"service-desc\", <%s>; rel=\"service-meta\"",
				server.URL+"/openapi.json",
				server.URL+"/metadata.json",
			),
		)
		w.WriteHeader(http.StatusOK)
	})

	store, err := cache.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	fetcher := cache.NewFetcher(cache.FetcherOptions{
		Store:  store,
		Client: server.Client(),
		Now: func() time.Time {
			return time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)
		},
	})

	if _, err := discovery.DiscoverServiceRoot(context.Background(), fetcher, server.URL+"/service", cache.Policy{}); err != nil {
		t.Fatalf("first DiscoverServiceRoot: %v", err)
	}

	result, err := discovery.DiscoverServiceRoot(context.Background(), fetcher, server.URL+"/service", cache.Policy{ForceRefresh: true})
	if err != nil {
		t.Fatalf("second DiscoverServiceRoot: %v", err)
	}
	if !sawConditional {
		t.Fatalf("expected conditional revalidation request")
	}
	if result.Provenance.CacheOutcome != "revalidated_hit" {
		t.Fatalf("expected revalidated provenance, got %#v", result.Provenance)
	}
}

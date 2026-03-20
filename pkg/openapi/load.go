package openapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/StevenBuglione/open-cli/pkg/cache"
	"github.com/StevenBuglione/open-cli/pkg/overlay"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

type LoadedDocument struct {
	Raw         map[string]any
	Document    *openapi3.T
	Fingerprint string
	Fetches     []FetchRecord
}

type FetchRecord struct {
	Outcome  cache.Outcome
	Metadata cache.Metadata
}

func LoadDocument(ctx context.Context, baseDir, ref string, overlays []string, fetcher *cache.Fetcher, policy cache.Policy) (*LoadedDocument, error) {
	raw, fingerprint, primaryFetch, err := loadAny(ctx, resolveReference(baseDir, ref), fetcher, policy)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	hash.Write([]byte(fingerprint))
	var fetches []FetchRecord
	if primaryFetch != nil {
		fetches = append(fetches, *primaryFetch)
	}

	for _, overlayRef := range overlays {
		path := resolveReference(baseDir, overlayRef)
		doc, fetchRecord, err := loadOverlay(ctx, path, fetcher, policy)
		if err != nil {
			return nil, err
		}
		raw, err = overlay.Apply(raw, doc)
		if err != nil {
			return nil, err
		}
		hash.Write([]byte(path))
		if fetchRecord != nil {
			fetches = append(fetches, *fetchRecord)
		}
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	document, err := loader.LoadFromData(data)
	if err != nil {
		return nil, err
	}
	if err := normalizeDocumentServers(document, resolveReference(baseDir, ref)); err != nil {
		return nil, err
	}
	if err := document.Validate(ctx); err != nil {
		return nil, err
	}

	return &LoadedDocument{
		Raw:         raw,
		Document:    document,
		Fingerprint: hex.EncodeToString(hash.Sum(nil)),
		Fetches:     fetches,
	}, nil
}

func ResolveReference(baseDir, ref string) string {
	return resolveReference(baseDir, ref)
}

func resolveReference(baseDir, ref string) string {
	if ref == "" {
		return ref
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "file://") {
		return ref
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(baseDir, ref)
}

func loadAny(ctx context.Context, ref string, fetcher *cache.Fetcher, policy cache.Policy) (map[string]any, string, *FetchRecord, error) {
	data, fetchRecord, err := ReadReference(ctx, ref, fetcher, policy)
	if err != nil {
		return nil, "", nil, err
	}

	var decoded any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, "", nil, err
	}
	normalized := normalize(decoded)
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, "", nil, fmt.Errorf("expected object document at %s", ref)
	}
	return object, string(data), fetchRecord, nil
}

func ReadReference(ctx context.Context, ref string, fetcher *cache.Fetcher, policy cache.Policy) ([]byte, *FetchRecord, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, nil, err
		}
		if fetcher == nil {
			fetcher = cache.NewFetcher(cache.FetcherOptions{})
		}
		result, err := fetcher.Fetch(req, cache.FetchOptions{Policy: policy})
		if err != nil {
			return nil, nil, err
		}
		return result.Body, &FetchRecord{Outcome: result.Outcome, Metadata: result.Metadata}, nil
	}
	if strings.HasPrefix(ref, "file://") {
		parsed, err := url.Parse(ref)
		if err != nil {
			return nil, nil, err
		}
		ref = parsed.Path
	}
	data, err := os.ReadFile(ref)
	return data, nil, err
}

func loadOverlay(ctx context.Context, ref string, fetcher *cache.Fetcher, policy cache.Policy) (overlay.Document, *FetchRecord, error) {
	data, fetchRecord, err := ReadReference(ctx, ref, fetcher, policy)
	if err != nil {
		return overlay.Document{}, nil, err
	}

	var doc overlay.Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return overlay.Document{}, nil, err
	}
	return doc, fetchRecord, nil
}

func normalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, inner := range typed {
			normalized[key] = normalize(inner)
		}
		return normalized
	case map[any]any:
		normalized := make(map[string]any, len(typed))
		for key, inner := range typed {
			normalized[fmt.Sprint(key)] = normalize(inner)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for idx, inner := range typed {
			normalized[idx] = normalize(inner)
		}
		return normalized
	default:
		return typed
	}
}

var serverVariablePattern = regexp.MustCompile(`\{([^{}]+)\}`)

func normalizeDocumentServers(document *openapi3.T, sourceRef string) error {
	baseURL, remote := remoteBaseURL(sourceRef)
	if remote && len(document.Servers) == 0 {
		document.Servers = openapi3.Servers{
			&openapi3.Server{URL: (&url.URL{Scheme: baseURL.Scheme, Host: baseURL.Host}).String()},
		}
	}
	if err := normalizeServerList(document.Servers, baseURL); err != nil {
		return err
	}
	if document.Paths == nil {
		return nil
	}
	for _, item := range document.Paths.Map() {
		if item == nil {
			continue
		}
		if err := normalizeServerList(item.Servers, baseURL); err != nil {
			return err
		}
		for _, operation := range []*openapi3.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete, item.Head, item.Options} {
			if operation == nil {
				continue
			}
			if operation.Servers != nil {
				if err := normalizeServerList(*operation.Servers, baseURL); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func remoteBaseURL(sourceRef string) (*url.URL, bool) {
	parsed, err := url.Parse(sourceRef)
	if err != nil {
		return nil, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false
	}
	return parsed, true
}

func normalizeServerList(servers openapi3.Servers, baseURL *url.URL) error {
	for idx, server := range servers {
		if server == nil {
			continue
		}
		normalized, err := normalizeServerURL(server, baseURL)
		if err != nil {
			if idx == 0 {
				return err
			}
			continue
		}
		server.URL = normalized
	}
	return nil
}

func normalizeServerURL(server *openapi3.Server, baseURL *url.URL) (string, error) {
	expanded, err := expandServerURL(server.URL, server.Variables)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(expanded)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() || baseURL == nil {
		return parsed.String(), nil
	}
	return baseURL.ResolveReference(parsed).String(), nil
}

func expandServerURL(raw string, variables map[string]*openapi3.ServerVariable) (string, error) {
	matches := serverVariablePattern.FindAllStringSubmatch(raw, -1)
	expanded := raw
	for _, match := range matches {
		name := match[1]
		variable, ok := variables[name]
		if !ok || variable == nil || strings.TrimSpace(variable.Default) == "" {
			return "", fmt.Errorf("server variable %q must define a default", name)
		}
		expanded = strings.ReplaceAll(expanded, match[0], variable.Default)
	}
	return expanded, nil
}

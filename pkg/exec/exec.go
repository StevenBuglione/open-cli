package exec

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
)

type AuthScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme,omitempty"`
	In     string `json:"in,omitempty"`
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
}

type AuthApplicationPlan struct {
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
}

type Request struct {
	BaseURL  string              `json:"baseUrl,omitempty"`
	Tool     catalog.Tool        `json:"tool"`
	PathArgs []string            `json:"pathArgs,omitempty"`
	Flags    map[string]string   `json:"flags,omitempty"`
	Body     []byte              `json:"body,omitempty"`
	Auth     []AuthScheme        `json:"auth,omitempty"`
	AuthPlan AuthApplicationPlan `json:"authPlan,omitempty"`
}

type Result struct {
	StatusCode int         `json:"statusCode"`
	Headers    http.Header `json:"headers,omitempty"`
	Body       []byte      `json:"body,omitempty"`
	RetryCount int         `json:"retryCount"`
}

func Execute(ctx context.Context, client *http.Client, request Request) (*Result, error) {
	if client == nil {
		client = http.DefaultClient
	}

	baseURL := request.BaseURL
	if baseURL == "" && len(request.Tool.Servers) > 0 {
		baseURL = request.Tool.Servers[0]
	}
	if baseURL == "" {
		return nil, fmt.Errorf("missing base url for tool %s", request.Tool.ID)
	}

	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + buildPath(request.Tool.Path, request.Tool.PathParams, request.PathArgs))
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	headers := http.Header{}
	for _, flag := range request.Tool.Flags {
		value, ok := request.Flags[flag.Name]
		if !ok {
			value, ok = request.Flags[flag.OriginalName]
		}
		if !ok {
			continue
		}
		switch flag.Location {
		case "query":
			query.Set(flag.OriginalName, value)
		case "header":
			headers.Set(flag.OriginalName, value)
		case "cookie":
			headers.Add("Cookie", fmt.Sprintf("%s=%s", flag.OriginalName, value))
		}
	}
	endpoint.RawQuery = query.Encode()

	var lastStatus int
	var lastBody []byte
	var lastHeaders http.Header
	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var body io.Reader
		if len(request.Body) > 0 {
			body = bytes.NewReader(request.Body)
		}
		req, err := http.NewRequestWithContext(ctx, request.Tool.Method, endpoint.String(), body)
		if err != nil {
			return nil, err
		}
		req.Header = headers.Clone()
		if len(request.Body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		applyAuth(req, request.Auth)
		applyAuthPlan(req, request.AuthPlan)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		responseBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		lastStatus = resp.StatusCode
		lastHeaders = resp.Header.Clone()
		lastBody = responseBody
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
			return &Result{
				StatusCode: resp.StatusCode,
				Headers:    lastHeaders,
				Body:       lastBody,
				RetryCount: attempt,
			}, nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return &Result{
		StatusCode: lastStatus,
		Headers:    lastHeaders,
		Body:       lastBody,
		RetryCount: maxAttempts - 1,
	}, nil
}

func buildPath(template string, params []catalog.Parameter, values []string) string {
	path := template
	for idx, parameter := range params {
		if idx >= len(values) {
			break
		}
		path = strings.ReplaceAll(path, "{"+parameter.OriginalName+"}", url.PathEscape(values[idx]))
	}
	return path
}

func applyAuth(req *http.Request, auth []AuthScheme) {
	for _, scheme := range auth {
		switch {
		case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer"):
			req.Header.Set("Authorization", "Bearer "+scheme.Value)
		case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "basic"):
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(scheme.Value)))
		case scheme.Type == "apiKey" && scheme.In == "header":
			req.Header.Set(scheme.Name, scheme.Value)
		case scheme.Type == "apiKey" && scheme.In == "query":
			query := req.URL.Query()
			query.Set(scheme.Name, scheme.Value)
			req.URL.RawQuery = query.Encode()
		}
	}
}

func applyAuthPlan(req *http.Request, plan AuthApplicationPlan) {
	for name, value := range plan.Headers {
		req.Header.Set(name, value)
	}
	if len(plan.Query) == 0 {
		return
	}
	query := req.URL.Query()
	for name, value := range plan.Query {
		query.Set(name, value)
	}
	req.URL.RawQuery = query.Encode()
}

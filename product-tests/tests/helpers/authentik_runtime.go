package helpers

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	runtimeserver "github.com/StevenBuglione/oas-cli-go/internal/runtime"
)

const (
	authentikRuntimeAudience   = "oasclird"
	authentikPrimarySlug       = "oascli-runtime"
	authentikAlternateSlug     = "oascli-runtime-alt-issuer"
	authentikCallbackURL       = "http://127.0.0.1:8787/callback"
	authentikRunTestsEnv       = "OASCLI_RUN_AUTHENTIK_TESTS"
	authentikClientIDEnv       = "OAS_REMOTE_CLIENT_ID"
	authentikClientSecretEnv   = "OAS_REMOTE_CLIENT_SECRET"
	authentikDefaultTTL        = "hours=1"
	authentikExpiredTokenTTL   = "seconds=1"
	authentikWrongAudienceFlag = "profile:wrong-audience"
)

type AuthentikRuntimeFixture struct {
	RuntimeURL string
	ConfigPath string
	primary    authentikClient
	alternate  authentikClient
	workerID   string
	cliEnv     []string
	httpClient *http.Client
	authProxy  *httptest.Server
}

type authentikClient struct {
	issuer           string
	discoveryURL     string
	jwksURL          string
	authorizationURL string
	tokenURL         string
	clientID         string
	clientSecret     string
}

type authentikDiscovery struct {
	Issuer                string `json:"issuer"`
	JWKSURI               string `json:"jwks_uri"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

type authentikBootstrapResult struct {
	Primary   authentikBootstrapClient `json:"primary"`
	Alternate authentikBootstrapClient `json:"alternate"`
}

type authentikBootstrapClient struct {
	Slug         string `json:"slug"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func NewAuthentikRuntimeFixture(t *testing.T) *AuthentikRuntimeFixture {
	t.Helper()

	repoRoot := repoRoot(t)
	composeFile := filepath.Join(repoRoot, "product-tests", "authentik", "docker-compose.yml")
	if enabled := os.Getenv(authentikRunTestsEnv) == "1"; !enabled {
		_, _, message := resolveAuthentikFixtureAvailability("worker", "", false)
		t.Skip(message)
	}
	workerID := composeServiceContainerID(t, composeFile, "worker")
	insecureClient := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}

	bootstrap := bootstrapAuthentikRuntime(t, workerID)
	primaryDiscovery := waitForAuthentikDiscovery(t, insecureClient, bootstrap.Primary.Slug)
	alternateDiscovery := waitForAuthentikDiscovery(t, insecureClient, bootstrap.Alternate.Slug)
	waitForReachableTokenEndpoint(t, insecureClient, primaryDiscovery.TokenEndpoint)
	waitForReachableTokenEndpoint(t, insecureClient, alternateDiscovery.TokenEndpoint)
	waitForReachableJWKS(t, insecureClient, primaryDiscovery.JWKSURI)
	waitForReachableJWKS(t, insecureClient, alternateDiscovery.JWKSURI)

	proxy := newAuthentikEndpointProxy(t, insecureClient, map[string]string{
		"/primary/discovery": primaryDiscovery.Issuer + ".well-known/openid-configuration",
		"/primary/jwks":      primaryDiscovery.JWKSURI,
		"/primary/authorize": primaryDiscovery.AuthorizationEndpoint,
		"/primary/token":     primaryDiscovery.TokenEndpoint,
		"/alternate/jwks":    alternateDiscovery.JWKSURI,
		"/alternate/token":   alternateDiscovery.TokenEndpoint,
	})

	ticketsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{map[string]any{"id": "T-1"}},
			"total": 1,
		})
	}))
	t.Cleanup(ticketsAPI.Close)
	usersAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{map[string]any{"id": "U-1"}},
			"total": 1,
		})
	}))
	t.Cleanup(usersAPI.Close)

	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatalf("mkdir runtime root: %v", err)
	}

	server := runtimeserver.NewServer(runtimeserver.Options{
		AuditPath: filepath.Join(runtimeRoot, "audit.log"),
		StateDir:  filepath.Join(runtimeRoot, "state"),
		CacheDir:  filepath.Join(runtimeRoot, "cache"),
	})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	configPath := writeAuthentikRuntimeConfig(t, root, runtimeServer.URL, proxy.URL, primaryDiscovery, ticketsAPI.URL, usersAPI.URL)

	return &AuthentikRuntimeFixture{
		RuntimeURL: runtimeServer.URL,
		ConfigPath: configPath,
		primary: authentikClient{
			issuer:           primaryDiscovery.Issuer,
			discoveryURL:     proxy.URL + "/primary/discovery",
			jwksURL:          proxy.URL + "/primary/jwks",
			authorizationURL: proxy.URL + "/primary/authorize",
			tokenURL:         proxy.URL + "/primary/token",
			clientID:         bootstrap.Primary.ClientID,
			clientSecret:     bootstrap.Primary.ClientSecret,
		},
		alternate: authentikClient{
			issuer:       alternateDiscovery.Issuer,
			jwksURL:      proxy.URL + "/alternate/jwks",
			tokenURL:     proxy.URL + "/alternate/token",
			clientID:     bootstrap.Alternate.ClientID,
			clientSecret: bootstrap.Alternate.ClientSecret,
		},
		workerID:   workerID,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		authProxy:  proxy,
		cliEnv: []string{
			authentikClientIDEnv + "=" + bootstrap.Primary.ClientID,
			authentikClientSecretEnv + "=" + bootstrap.Primary.ClientSecret,
		},
	}
}

func (fixture *AuthentikRuntimeFixture) CLIEnv() []string {
	return append([]string(nil), fixture.cliEnv...)
}

func (fixture *AuthentikRuntimeFixture) AcquireClientCredentialsToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return fixture.acquireToken(t, fixture.primary.tokenURL, fixture.primary.clientID, fixture.primary.clientSecret, scopes...)
}

func (fixture *AuthentikRuntimeFixture) AcquireAlternateIssuerToken(t *testing.T, scopes ...string) string {
	t.Helper()
	return fixture.acquireToken(t, fixture.alternate.tokenURL, fixture.alternate.clientID, fixture.alternate.clientSecret, scopes...)
}

func (fixture *AuthentikRuntimeFixture) AcquireExpiredClientCredentialsToken(t *testing.T, scopes ...string) string {
	t.Helper()

	fixture.setProviderAccessTokenValidity(t, authentikPrimarySlug, authentikExpiredTokenTTL)
	t.Cleanup(func() {
		fixture.setProviderAccessTokenValidity(t, authentikPrimarySlug, authentikDefaultTTL)
	})

	token := fixture.acquireToken(t, fixture.primary.tokenURL, fixture.primary.clientID, fixture.primary.clientSecret, scopes...)
	time.Sleep(2 * time.Second)
	return token
}

func (fixture *AuthentikRuntimeFixture) acquireToken(t *testing.T, tokenURL, clientID, clientSecret string, scopes ...string) string {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("audience", authentikRuntimeAudience)
	form.Set("scope", strings.Join(scopes, " "))
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := fixture.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request client credentials token: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read token response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 client credentials response, got %d with body %s", resp.StatusCode, string(body))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		t.Fatalf("expected access token in response, got %s", string(body))
	}
	return payload.AccessToken
}

func (fixture *AuthentikRuntimeFixture) setProviderAccessTokenValidity(t *testing.T, slug, validity string) {
	t.Helper()

	script := fmt.Sprintf(`
from authentik.core.models import Application
from authentik.providers.oauth2.models import OAuth2Provider

application = Application.objects.get(slug=%q)
provider = OAuth2Provider.objects.get(pk=application.provider_id)
provider.access_token_validity = %q
provider.save(update_fields=["access_token_validity"])
print("__OASCLI_JSON__=" + %q)
`, slug, validity, validity)
	if _, err := runAuthentikManageScript(t, fixture.workerID, script); err != nil {
		t.Fatalf("set provider access token validity for %s: %v", slug, err)
	}
}

func writeAuthentikRuntimeConfig(t *testing.T, dir, runtimeURL, proxyURL string, discovery authentikDiscovery, ticketsURL, usersURL string) string {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "tickets.openapi.yaml"), []byte(fmt.Sprintf(`
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: %s
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`, ticketsURL)), 0o644); err != nil {
		t.Fatalf("write tickets openapi: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.openapi.yaml"), []byte(fmt.Sprintf(`
openapi: 3.1.0
info:
  title: Users API
  version: "1.0.0"
servers:
  - url: %s
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      responses:
        "200":
          description: OK
`, usersURL)), 0o644); err != nil {
		t.Fatalf("write users openapi: %v", err)
	}

	templatePath := filepath.Join(repoRoot(t), "examples", "runtime-auth-broker", "authentik", "runtime.oauth-client.cli.json.tmpl")
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read runtime config template: %v", err)
	}
	rendered := strings.NewReplacer(
		"${AUTHENTIK_ISSUER}", discovery.Issuer,
		"${AUTHENTIK_JWKS_URL}", proxyURL+"/primary/jwks",
		"${RUNTIME_AUDIENCE}", authentikRuntimeAudience,
		"${AUTHENTIK_TOKEN_URL}", proxyURL+"/primary/token",
		"${RUNTIME_URL}", runtimeURL,
	).Replace(string(templateBytes))

	var config map[string]any
	if err := json.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatalf("decode rendered config template: %v", err)
	}
	runtimeConfig, _ := config["runtime"].(map[string]any)
	runtimeConfig["mode"] = "remote"
	runtimeConfig["remote"] = map[string]any{
		"url": runtimeURL,
		"oauth": map[string]any{
			"mode":     "oauthClient",
			"audience": authentikRuntimeAudience,
			"scopes":   []string{"bundle:tickets", "tool:tickets:listTickets"},
			"client": map[string]any{
				"tokenURL":     proxyURL + "/primary/token",
				"clientId":     map[string]any{"type": "env", "value": authentikClientIDEnv},
				"clientSecret": map[string]any{"type": "env", "value": authentikClientSecretEnv},
			},
		},
	}
	sources, _ := config["sources"].(map[string]any)
	sources["ticketsSource"] = map[string]any{
		"type":    "openapi",
		"uri":     "./tickets.openapi.yaml",
		"enabled": true,
	}
	sources["usersSource"] = map[string]any{
		"type":    "openapi",
		"uri":     "./users.openapi.yaml",
		"enabled": true,
	}
	services, _ := config["services"].(map[string]any)
	services["tickets"] = map[string]any{
		"source": "ticketsSource",
		"alias":  "tickets",
	}
	services["users"] = map[string]any{
		"source": "usersSource",
		"alias":  "users",
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("marshal Authentik runtime config: %v", err)
	}
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write Authentik runtime config: %v", err)
	}
	return configPath
}

func newAuthentikEndpointProxy(t *testing.T, upstreamClient *http.Client, routes map[string]string) *httptest.Server {
	t.Helper()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var body io.Reader
		if r.Body != nil {
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for key, values := range r.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		resp, err := upstreamClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func bootstrapAuthentikRuntime(t *testing.T, workerID string) authentikBootstrapResult {
	t.Helper()

	scopeExpression := `
audience = "wrong-audience" if "profile:wrong-audience" in token.scope else "oasclird"
return {"scope": " ".join(token.scope), "aud": audience}
`
	script := fmt.Sprintf(`
import json
from authentik.core.models import Application
from authentik.crypto.models import CertificateKeyPair
from authentik.flows.models import Flow
from authentik.providers.oauth2.models import OAuth2Provider, RedirectURI, RedirectURIMatchingMode, ScopeMapping

scope_expression = %q

auth_flow = Flow.objects.get(slug="default-authentication-flow")
authorization_flow = Flow.objects.get(slug="default-provider-authorization-implicit-consent")
invalidation_flow = Flow.objects.get(slug="default-provider-invalidation-flow")
signing_key = CertificateKeyPair.objects.filter(name="authentik Self-signed Certificate").first()
if signing_key is None:
    signing_key = CertificateKeyPair.objects.filter(name="authentik Internal JWT Certificate").first()
if signing_key is None:
    signing_key = CertificateKeyPair.objects.order_by("name").first()
if signing_key is None:
    raise RuntimeError("no signing key available in Authentik")

def ensure_scope(name, scope_name):
    mapping, _ = ScopeMapping.objects.update_or_create(
        name=name,
        defaults={
            "scope_name": scope_name,
            "description": scope_name,
            "expression": scope_expression,
        },
    )
    return mapping

scope_mappings = [
    ensure_scope("oascli runtime bundle:tickets", "bundle:tickets"),
    ensure_scope("oascli runtime tool:tickets:listTickets", "tool:tickets:listTickets"),
    ensure_scope("oascli runtime tool:users:listUsers", "tool:users:listUsers"),
    ensure_scope("oascli runtime profile:wrong-audience", "profile:wrong-audience"),
]

def ensure_provider(provider_name, application_name, slug):
    provider = OAuth2Provider.objects.filter(name=provider_name).first()
    if provider is None:
        provider = OAuth2Provider(name=provider_name)
    provider.client_type = "confidential"
    provider.include_claims_in_id_token = True
    provider.access_token_validity = %q
    provider.refresh_token_validity = "days=30"
    provider.authentication_flow = auth_flow
    provider.authorization_flow = authorization_flow
    provider.invalidation_flow = invalidation_flow
    provider.signing_key = signing_key
    provider.redirect_uris = [
        RedirectURI(
            matching_mode=RedirectURIMatchingMode.STRICT,
            url=%q,
        )
    ]
    provider.save()
    provider.property_mappings.set(scope_mappings)
    application, _ = Application.objects.update_or_create(
        slug=slug,
        defaults={"name": application_name, "provider": provider},
    )
    if application.provider_id != provider.pk:
        application.provider = provider
        application.save(update_fields=["provider"])
    return {
        "slug": slug,
        "client_id": provider.client_id,
        "client_secret": provider.client_secret,
    }

result = {
    "primary": ensure_provider("oascli Runtime Provider", "oascli Runtime", %q),
    "alternate": ensure_provider("oascli Runtime Provider Alt Issuer", "oascli Runtime Alt Issuer", %q),
}
print("__OASCLI_JSON__=" + json.dumps(result))
`, scopeExpression, authentikDefaultTTL, authentikCallbackURL, authentikPrimarySlug, authentikAlternateSlug)

	var result authentikBootstrapResult
	waitForCondition(t, 2*time.Minute, func() (bool, error) {
		output, err := runAuthentikManageScript(t, workerID, script)
		if err != nil {
			return false, nil
		}
		if err := json.Unmarshal(output, &result); err != nil {
			return false, nil
		}
		return true, nil
	}, "bootstrap Authentik runtime provider")
	return result
}

func waitForAuthentikDiscovery(t *testing.T, client *http.Client, slug string) authentikDiscovery {
	t.Helper()

	var discovery authentikDiscovery
	url := "https://127.0.0.1:9444/application/o/" + slug + "/.well-known/openid-configuration"
	waitForCondition(t, 2*time.Minute, func() (bool, error) {
		resp, err := client.Get(url)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false, nil
		}
		if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
			return false, nil
		}
		return strings.TrimSpace(discovery.Issuer) != "" && strings.TrimSpace(discovery.JWKSURI) != "" && strings.TrimSpace(discovery.TokenEndpoint) != "", nil
	}, "wait for Authentik discovery "+slug)
	return discovery
}

func waitForReachableJWKS(t *testing.T, client *http.Client, jwksURL string) {
	t.Helper()

	waitForCondition(t, 2*time.Minute, func() (bool, error) {
		resp, err := client.Get(jwksURL)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK, nil
	}, "wait for Authentik JWKS")
}

func waitForReachableTokenEndpoint(t *testing.T, client *http.Client, tokenURL string) {
	t.Helper()

	waitForCondition(t, 2*time.Minute, func() (bool, error) {
		req, err := http.NewRequest(http.MethodGet, tokenURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusMethodNotAllowed, http.StatusBadRequest, http.StatusUnauthorized:
			return true, nil
		default:
			return false, nil
		}
	}, "wait for Authentik token endpoint")
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() (bool, error), description string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := check()
		if err != nil {
			lastErr = err
		}
		if ok {
			return
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("%s: %v", description, lastErr)
	}
	t.Fatalf("%s: timed out after %s", description, timeout)
}

func composeServiceContainerID(t *testing.T, composeFile, service string) string {
	t.Helper()

	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "-q", service)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve %s container id: %v\n%s", service, err, output)
	}
	containerID := strings.TrimSpace(string(output))
	ready, fatal, message := resolveAuthentikFixtureAvailability(service, containerID, true)
	if !ready {
		if fatal {
			t.Fatal(message)
		}
		t.Skip(message)
	}
	return containerID
}

func resolveAuthentikFixtureAvailability(service, containerID string, enableTests bool) (bool, bool, string) {
	if !enableTests {
		return false, false, "live Authentik product tests are disabled; set OASCLI_RUN_AUTHENTIK_TESTS=1 or use make test-runtime-auth-authentik"
	}
	if strings.TrimSpace(containerID) == "" {
		return false, true, fmt.Sprintf("%s container is not running; start the Authentik stack with product-tests/scripts/authentik-up.sh or make authentik-up", service)
	}
	return true, false, ""
}

func runAuthentikManageScript(t *testing.T, workerID, script string) ([]byte, error) {
	t.Helper()

	cmd := exec.Command("docker", "exec", workerID, "/ak-root/venv/bin/python", "/manage.py", "shell", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v\n%s", err, output)
	}
	const marker = "__OASCLI_JSON__="
	index := strings.LastIndex(string(output), marker)
	if index == -1 {
		return nil, fmt.Errorf("marker %q not found in output\n%s", marker, output)
	}
	return []byte(strings.TrimSpace(string(output[index+len(marker):]))), nil
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve source location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

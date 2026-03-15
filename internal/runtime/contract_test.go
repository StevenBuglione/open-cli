package runtime_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
)

// ── ContractVersion parsing ──────────────────────────────────────────────────

func TestContractVersionParseAndString(t *testing.T) {
	cv, err := runtime.ParseContractVersion("2.3")
	if err != nil {
		t.Fatalf("ParseContractVersion: %v", err)
	}
	if cv.Major != 2 || cv.Minor != 3 {
		t.Fatalf("expected 2.3, got %v", cv)
	}
	if cv.String() != "2.3" {
		t.Fatalf("expected String()=2.3, got %q", cv.String())
	}
}

func TestContractVersionParseRejectsInvalid(t *testing.T) {
	for _, bad := range []string{"", "1", "a.b", "1.2.3", "-1.0"} {
		if _, err := runtime.ParseContractVersion(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

// ── CheckCompatibility ───────────────────────────────────────────────────────

func TestCheckCompatibilityMatchingVersionAndCapabilities(t *testing.T) {
	server := runtime.HandshakeInfo{
		ContractVersion: "1.0",
		Capabilities:    []string{"catalog", "execute"},
	}
	client := runtime.HandshakeInfo{
		ContractVersion:      "1.0",
		RequiredCapabilities: []string{"catalog"},
	}
	if err := runtime.CheckCompatibility(client, server); err != nil {
		t.Fatalf("expected compatible, got: %v", err)
	}
}

func TestCheckCompatibilityMajorMismatchFails(t *testing.T) {
	server := runtime.HandshakeInfo{
		ContractVersion: "1.0",
		Capabilities:    []string{"catalog"},
	}
	client := runtime.HandshakeInfo{
		ContractVersion: "2.0",
	}
	err := runtime.CheckCompatibility(client, server)
	if err == nil {
		t.Fatal("expected contract_mismatch error for major version mismatch")
	}
	var cmErr *runtime.ContractMismatchError
	if !errors.As(err, &cmErr) {
		t.Fatalf("expected *ContractMismatchError, got %T: %v", err, err)
	}
	if cmErr.Code() != "contract_mismatch" {
		t.Fatalf("expected code=contract_mismatch, got %q", cmErr.Code())
	}
}

func TestCheckCompatibilityMinorDiffWithAllCapsPresent(t *testing.T) {
	server := runtime.HandshakeInfo{
		ContractVersion: "1.2",
		Capabilities:    []string{"catalog", "execute", "refresh"},
	}
	client := runtime.HandshakeInfo{
		ContractVersion:      "1.0",
		RequiredCapabilities: []string{"catalog", "execute"},
	}
	if err := runtime.CheckCompatibility(client, server); err != nil {
		t.Fatalf("expected compatible with minor diff + all caps present, got: %v", err)
	}
}

func TestCheckCompatibilityMinorDiffMissingRequiredCapFails(t *testing.T) {
	server := runtime.HandshakeInfo{
		ContractVersion: "1.1",
		Capabilities:    []string{"catalog"},
	}
	client := runtime.HandshakeInfo{
		ContractVersion:      "1.0",
		RequiredCapabilities: []string{"catalog", "execute"},
	}
	err := runtime.CheckCompatibility(client, server)
	if err == nil {
		t.Fatal("expected contract_mismatch error for missing required capability")
	}
	var cmErr *runtime.ContractMismatchError
	if !errors.As(err, &cmErr) {
		t.Fatalf("expected *ContractMismatchError, got %T: %v", err, err)
	}
	if cmErr.Code() != "contract_mismatch" {
		t.Fatalf("expected code=contract_mismatch, got %q", cmErr.Code())
	}
}

func TestCheckCompatibilityInvalidContractVersionFails(t *testing.T) {
	server := runtime.HandshakeInfo{ContractVersion: "bad"}
	client := runtime.HandshakeInfo{ContractVersion: "1.0"}
	if err := runtime.CheckCompatibility(client, server); err == nil {
		t.Fatal("expected error for invalid server contract version")
	}

	server2 := runtime.HandshakeInfo{ContractVersion: "1.0"}
	client2 := runtime.HandshakeInfo{ContractVersion: "bad"}
	if err := runtime.CheckCompatibility(client2, server2); err == nil {
		t.Fatal("expected error for invalid client contract version")
	}
}

// ── /v1/runtime/info handshake surface ──────────────────────────────────────

func TestRuntimeInfoEndpointReturnsHandshakeInfo(t *testing.T) {
	dir := t.TempDir()
	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/runtime/info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Decode as HandshakeInfo; extra fields (instanceId, lifecycle, etc.) are ignored.
	var info runtime.HandshakeInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode HandshakeInfo: %v", err)
	}
	if info.ContractVersion == "" {
		t.Fatal("expected non-empty contractVersion in handshake response")
	}
	if _, err := runtime.ParseContractVersion(info.ContractVersion); err != nil {
		t.Fatalf("contractVersion %q is not a valid version: %v", info.ContractVersion, err)
	}
	if len(info.Capabilities) == 0 {
		t.Fatal("expected at least one capability in handshake response")
	}
}

func TestRuntimeInfoEndpointOnlyAcceptsGET(t *testing.T) {
	dir := t.TempDir()
	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected non-200 for POST to /v1/runtime/info")
	}
}

// TestRuntimeInfoContractVersionCompatibleWithServerCapabilities verifies that
// the advertised CurrentContractVersion and ServerCapabilities round-trip through
// CheckCompatibility when a client requires all server-advertised capabilities.
func TestRuntimeInfoContractVersionCompatibleWithServerCapabilities(t *testing.T) {
	dir := t.TempDir()
	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
	})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/runtime/info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var info runtime.HandshakeInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode HandshakeInfo: %v", err)
	}

	client := runtime.HandshakeInfo{
		ContractVersion:      info.ContractVersion,
		RequiredCapabilities: info.Capabilities,
	}
	if err := runtime.CheckCompatibility(client, info); err != nil {
		t.Fatalf("server should satisfy its own advertised capabilities: %v", err)
	}
}

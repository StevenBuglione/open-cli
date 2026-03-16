package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// CurrentContractVersion is the contract version advertised by this runtime server.
const CurrentContractVersion = "1.1"

// CurrentAuthorizationEnvelopeVersion is the version advertised for the
// runtime authorization envelope used by catalog filtering and execution.
const CurrentAuthorizationEnvelopeVersion = "1.0"

// ServerCapabilities are the capabilities advertised by this runtime server.
var ServerCapabilities = []string{
	"catalog",
	"execute",
	"refresh",
	"audit",
	"brokered-auth",
	"authorization-envelope",
}

// AuthScopePrefixes are the canonical scope prefixes recognized by the runtime
// when resolving catalog and execution authorization envelopes.
var AuthScopePrefixes = []string{"bundle:", "profile:", "tool:"}

// ContractVersion is a major.minor contract version pair.
type ContractVersion struct {
	Major uint
	Minor uint
}

// ParseContractVersion parses a "major.minor" string into a ContractVersion.
// It returns an error for any format other than two non-negative integers separated by ".".
func ParseContractVersion(s string) (ContractVersion, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return ContractVersion{}, fmt.Errorf("contract version %q must be of the form MAJOR.MINOR", s)
	}
	major, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || parts[0] == "" {
		return ContractVersion{}, fmt.Errorf("contract version %q: invalid major component", s)
	}
	minor, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || parts[1] == "" {
		return ContractVersion{}, fmt.Errorf("contract version %q: invalid minor component", s)
	}
	return ContractVersion{Major: uint(major), Minor: uint(minor)}, nil
}

// String returns the "major.minor" representation.
func (cv ContractVersion) String() string {
	return fmt.Sprintf("%d.%d", cv.Major, cv.Minor)
}

// HandshakeInfo is the payload returned by the /v1/runtime/info endpoint and
// also used by clients to describe their version requirements.
//
// Server response fields: ContractVersion, Capabilities.
// Client request fields:  ContractVersion, RequiredCapabilities.
type HandshakeInfo struct {
	// ContractVersion is the server's advertised (or client's expected) version string.
	ContractVersion string `json:"contractVersion"`
	// Capabilities is the set of feature names the server supports.
	Capabilities []string `json:"capabilities,omitempty"`
	// RequiredCapabilities is used client-side to declare which capabilities are mandatory.
	// It is omitted from server responses.
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
	// Auth advertises runtime auth requirements, diagnostics, and compatibility
	// metadata for brokered authentication flows.
	Auth *AuthHandshakeInfo `json:"auth,omitempty"`
}

// AuthHandshakeInfo describes the auth-specific handshake metadata advertised by
// the runtime.
type AuthHandshakeInfo struct {
	Required bool   `json:"required"`
	Audience string `json:"audience,omitempty"`
	// ScopePrefixes are the recognized prefixes for scopes that participate in
	// the runtime authorization envelope.
	ScopePrefixes []string `json:"scopePrefixes,omitempty"`
	// TokenValidationProfiles advertises the configured validation profiles used
	// to authenticate bearer tokens at the runtime boundary.
	TokenValidationProfiles []string                            `json:"tokenValidationProfiles,omitempty"`
	BrowserLogin            *BrowserLoginHandshakeInfo          `json:"browserLogin,omitempty"`
	Principal               string                              `json:"principal,omitempty"`
	SessionID               string                              `json:"sessionId,omitempty"`
	AuthorizationEnvelope   *AuthorizationEnvelopeHandshakeInfo `json:"authorizationEnvelope,omitempty"`
}

// BrowserLoginHandshakeInfo describes the browser-login discovery surface for
// remote runtime auth.
type BrowserLoginHandshakeInfo struct {
	Configured     bool   `json:"configured"`
	ConfigEndpoint string `json:"configEndpoint,omitempty"`
}

// AuthorizationEnvelopeHandshakeInfo describes the versioned authorization
// envelope metadata used to keep catalog filtering and execution parity.
type AuthorizationEnvelopeHandshakeInfo struct {
	Version       string   `json:"version"`
	ScopePrefixes []string `json:"scopePrefixes,omitempty"`
}

// ContractMismatchError is returned by CheckCompatibility when the client and
// server runtime contracts are incompatible.
type ContractMismatchError struct {
	msg string
}

// Code returns the structured error category string used by the runtime error taxonomy.
func (e *ContractMismatchError) Code() string { return "contract_mismatch" }

func (e *ContractMismatchError) Error() string { return e.msg }

// CheckCompatibility validates that the client's contract requirements are
// satisfied by the server's advertised handshake info.
//
// Rules (from spec):
//   - major version must match exactly
//   - minor-version differences are allowed only when all client RequiredCapabilities
//     are advertised by the server
//   - any other combination fails with a *ContractMismatchError
func CheckCompatibility(client, server HandshakeInfo) error {
	serverCV, err := ParseContractVersion(server.ContractVersion)
	if err != nil {
		return fmt.Errorf("invalid server contractVersion: %w", err)
	}
	clientCV, err := ParseContractVersion(client.ContractVersion)
	if err != nil {
		return fmt.Errorf("invalid client contractVersion: %w", err)
	}

	if clientCV.Major != serverCV.Major {
		return &ContractMismatchError{
			msg: fmt.Sprintf(
				"contract_mismatch: major version mismatch (client=%s, server=%s)",
				client.ContractVersion, server.ContractVersion,
			),
		}
	}

	serverCaps := make(map[string]struct{}, len(server.Capabilities))
	for _, cap := range server.Capabilities {
		serverCaps[cap] = struct{}{}
	}

	var missing []string
	for _, required := range client.RequiredCapabilities {
		if _, ok := serverCaps[required]; !ok {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return &ContractMismatchError{
			msg: fmt.Sprintf(
				"contract_mismatch: server (version=%s) is missing required capabilities: %s",
				server.ContractVersion, strings.Join(missing, ", "),
			),
		}
	}

	return nil
}

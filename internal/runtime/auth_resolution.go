package runtime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	oauth "github.com/StevenBuglione/open-cli/pkg/auth"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
	httpexec "github.com/StevenBuglione/open-cli/pkg/exec"
)

type authTarget struct {
	kind string
	name string
}

type alternativeAttempt struct {
	schemeNames []string
	err         error
}

func (server *Server) resolveAuthPlan(ctx context.Context, cfg config.Config, tool catalog.Tool) (httpexec.AuthApplicationPlan, error) {
	attempts := []alternativeAttempt{}
	for _, interactive := range []bool{false, true} {
		for _, alternative := range authAlternativesForTool(tool) {
			plan, attempt, ok := server.tryResolveAlternative(ctx, cfg, tool.ServiceID, alternative, interactive)
			if ok {
				return plan, nil
			}
			attempts = append(attempts, attempt)
		}
	}
	return httpexec.AuthApplicationPlan{}, joinAlternativeErrors(attempts)
}

func authAlternativesForTool(tool catalog.Tool) []catalog.AuthAlternative {
	if len(tool.AuthAlternatives) > 0 {
		return tool.AuthAlternatives
	}
	if len(tool.Auth) == 0 {
		return nil
	}
	return []catalog.AuthAlternative{{Requirements: append([]catalog.AuthRequirement(nil), tool.Auth...)}}
}

func (server *Server) tryResolveAlternative(ctx context.Context, cfg config.Config, serviceID string, alternative catalog.AuthAlternative, interactive bool) (httpexec.AuthApplicationPlan, alternativeAttempt, bool) {
	plan := httpexec.AuthApplicationPlan{
		Headers: map[string]string{},
		Query:   map[string]string{},
	}
	attempt := alternativeAttempt{}
	for _, requirement := range alternative.Requirements {
		attempt.schemeNames = append(attempt.schemeNames, requirement.Name)
		value, target, err := server.resolveRequirement(ctx, cfg, serviceID, requirement, interactive)
		if err != nil {
			attempt.err = err
			return httpexec.AuthApplicationPlan{}, attempt, false
		}
		switch target.kind {
		case "header":
			if prior, ok := plan.Headers[target.name]; ok && prior != value {
				attempt.err = fmt.Errorf("conflicting header auth target %s", target.name)
				return httpexec.AuthApplicationPlan{}, attempt, false
			}
			plan.Headers[target.name] = value
		case "query":
			if prior, ok := plan.Query[target.name]; ok && prior != value {
				attempt.err = fmt.Errorf("conflicting query auth target %s", target.name)
				return httpexec.AuthApplicationPlan{}, attempt, false
			}
			plan.Query[target.name] = value
		default:
			attempt.err = fmt.Errorf("unsupported auth target %s", target.kind)
			return httpexec.AuthApplicationPlan{}, attempt, false
		}
	}
	return plan, attempt, true
}

func (server *Server) resolveRequirement(ctx context.Context, cfg config.Config, serviceID string, requirement catalog.AuthRequirement, interactive bool) (string, authTarget, error) {
	secretKey, secret, ok := lookupSecret(cfg.Secrets, serviceID, requirement.Name)
	if !ok {
		return "", authTarget{}, fmt.Errorf("missing secret for %s", requirement.Name)
	}
	if requirement.Type == "oauth2" || requirement.Type == "openIdConnect" {
		token, err := oauth.ResolveOAuthAccessTokenWithOptions(ctx, server.client, cfg.Policy, secret, requirement, secretKey, server.stateDir, server.keychainResolver, oauth.ResolveOptions{
			Interactive: interactive,
		})
		if err != nil {
			return "", authTarget{}, err
		}
		return "Bearer " + token, authTarget{kind: "header", name: "Authorization"}, nil
	}

	value, err := resolveSecret(cfg.Policy, secret, server.keychainResolver)
	if err != nil {
		return "", authTarget{}, err
	}
	switch requirement.Type {
	case "apiKey":
		switch requirement.In {
		case "header":
			return value, authTarget{kind: "header", name: requirement.ParamName}, nil
		case "query":
			return value, authTarget{kind: "query", name: requirement.ParamName}, nil
		}
	case "http":
		switch strings.ToLower(requirement.Scheme) {
		case "basic":
			return "Basic " + base64.StdEncoding.EncodeToString([]byte(value)), authTarget{kind: "header", name: "Authorization"}, nil
		case "bearer":
			return "Bearer " + value, authTarget{kind: "header", name: "Authorization"}, nil
		}
	}
	return "", authTarget{}, fmt.Errorf("unsupported auth requirement %s/%s", requirement.Type, requirement.Scheme)
}

func joinAlternativeErrors(attempts []alternativeAttempt) error {
	if len(attempts) == 0 {
		return nil
	}
	var parts []string
	for _, attempt := range attempts {
		if attempt.err == nil {
			continue
		}
		label := strings.Join(attempt.schemeNames, "+")
		if label == "" {
			label = "alternative"
		}
		parts = append(parts, fmt.Sprintf("%s: %v", label, attempt.err))
	}
	if len(parts) == 0 {
		return errors.New("no auth alternative could be resolved")
	}
	return errors.New(strings.Join(parts, "; "))
}

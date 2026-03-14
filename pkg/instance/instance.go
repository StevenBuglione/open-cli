package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	InstanceID string
	ConfigPath string
	StateRoot  string
	CacheRoot  string
}

type Paths struct {
	InstanceID  string
	StateDir    string
	CacheDir    string
	AuditPath   string
	RuntimePath string
}

type RuntimeInfo struct {
	InstanceID string `json:"instanceId"`
	URL        string `json:"url"`
	AuditPath  string `json:"auditPath,omitempty"`
	CacheDir   string `json:"cacheDir,omitempty"`
}

func DeriveID(explicit, configPath string) string {
	if value := slugify(explicit); value != "" {
		return value
	}
	if configPath == "" {
		return "default"
	}
	normalized := configPath
	if abs, err := filepath.Abs(configPath); err == nil {
		normalized = abs
	}
	base := filepath.Base(filepath.Dir(normalized))
	if base == "." || base == string(filepath.Separator) {
		base = strings.TrimSuffix(filepath.Base(normalized), filepath.Ext(normalized))
	}
	base = slugify(base)
	if base == "" {
		base = "scope"
	}
	sum := sha256.Sum256([]byte(filepath.Clean(normalized)))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:])[:10])
}

func Resolve(options Options) (Paths, error) {
	instanceID := DeriveID(options.InstanceID, options.ConfigPath)
	stateRoot, err := resolveStateRoot(options.StateRoot)
	if err != nil {
		return Paths{}, err
	}
	cacheRoot, err := resolveCacheRoot(options.CacheRoot, options.StateRoot)
	if err != nil {
		return Paths{}, err
	}
	stateDir := filepath.Join(stateRoot, "instances", instanceID)
	return Paths{
		InstanceID:  instanceID,
		StateDir:    stateDir,
		CacheDir:    filepath.Join(cacheRoot, "instances", instanceID, "http"),
		AuditPath:   filepath.Join(stateDir, "audit.log"),
		RuntimePath: filepath.Join(stateDir, "runtime.json"),
	}, nil
}

func ReadRuntimeInfo(path string) (RuntimeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeInfo{}, err
	}
	var info RuntimeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return RuntimeInfo{}, err
	}
	return info, nil
}

func WriteRuntimeInfo(path string, info RuntimeInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func resolveStateRoot(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "oas-cli"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "oas-cli"), nil
}

func resolveCacheRoot(explicit, stateRoot string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if stateRoot != "" {
		return filepath.Join(stateRoot, "cache"), nil
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "oas-cli"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "oas-cli"), nil
}

func slugify(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, char := range value {
		switch {
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			builder.WriteRune(char)
		case char == '-' || char == '_' || char == ' ' || char == '.':
			if builder.Len() > 0 && builder.String()[builder.Len()-1] != '-' {
				builder.WriteByte('-')
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

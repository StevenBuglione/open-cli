package instance_test

import (
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/instance"
)

func TestDeriveIDUsesConfigPathWhenExplicitInstanceMissing(t *testing.T) {
	configA := filepath.Join("/tmp", "team-a", ".cli.json")
	configB := filepath.Join("/tmp", "team-b", ".cli.json")

	idA := instance.DeriveID("", configA)
	idB := instance.DeriveID("", configB)

	if idA == "" || idA == "default" {
		t.Fatalf("expected config-derived instance id, got %q", idA)
	}
	if idA != instance.DeriveID("", configA) {
		t.Fatalf("expected stable instance id derivation for %q", configA)
	}
	if idA == idB {
		t.Fatalf("expected different config paths to derive different instance ids, got %q", idA)
	}
	if got := instance.DeriveID("manual", configA); got != "manual" {
		t.Fatalf("expected explicit instance id to win, got %q", got)
	}
}

func TestResolvePathsIsolatesStatePerInstance(t *testing.T) {
	root := t.TempDir()

	alpha, err := instance.Resolve(instance.Options{
		InstanceID: "alpha",
		StateRoot:  filepath.Join(root, "state"),
		CacheRoot:  filepath.Join(root, "cache"),
	})
	if err != nil {
		t.Fatalf("Resolve alpha: %v", err)
	}
	beta, err := instance.Resolve(instance.Options{
		InstanceID: "beta",
		StateRoot:  filepath.Join(root, "state"),
		CacheRoot:  filepath.Join(root, "cache"),
	})
	if err != nil {
		t.Fatalf("Resolve beta: %v", err)
	}

	if alpha.AuditPath == beta.AuditPath {
		t.Fatalf("expected isolated audit paths, got %q", alpha.AuditPath)
	}
	if alpha.CacheDir == beta.CacheDir {
		t.Fatalf("expected isolated cache dirs, got %q", alpha.CacheDir)
	}
	if alpha.RuntimePath == beta.RuntimePath {
		t.Fatalf("expected isolated runtime metadata paths, got %q", alpha.RuntimePath)
	}
}

func TestRuntimeInfoRoundTrip(t *testing.T) {
	root := t.TempDir()

	paths, err := instance.Resolve(instance.Options{
		InstanceID: "alpha",
		StateRoot:  filepath.Join(root, "state"),
		CacheRoot:  filepath.Join(root, "cache"),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	info := instance.RuntimeInfo{
		InstanceID: "alpha",
		URL:        "http://127.0.0.1:40123",
		AuditPath:  paths.AuditPath,
		CacheDir:   paths.CacheDir,
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, info); err != nil {
		t.Fatalf("WriteRuntimeInfo: %v", err)
	}

	loaded, err := instance.ReadRuntimeInfo(paths.RuntimePath)
	if err != nil {
		t.Fatalf("ReadRuntimeInfo: %v", err)
	}
	if loaded.InstanceID != info.InstanceID || loaded.URL != info.URL {
		t.Fatalf("unexpected runtime info round trip: %#v", loaded)
	}
}

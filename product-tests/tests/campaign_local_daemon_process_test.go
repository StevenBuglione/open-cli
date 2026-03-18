package tests_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/instance"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignLocalDaemonProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("process-backed daemon campaign is disabled in short mode")
	}

	fr := helpers.NewFindingsRecorder("local-daemon-process")
	fr.SetLaneMetadata("product-validation", "local-daemon", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	t.Run("matrix-lane-registered", func(t *testing.T) {
		matrix, err := helpers.LoadCapabilityMatrix(filepath.Join("..", "testdata", "fleet", "capability-matrix.yaml"))
		if err != nil {
			t.Fatalf("load capability matrix: %v", err)
		}
		lane := findCapabilityLane(matrix, "local-daemon-process")
		if lane == nil {
			t.Fatalf("local-daemon-process lane is not registered in capability matrix")
		}
		fr.Check("matrix-process-lane", "capability matrix registers the process-backed daemon lane", "^TestCampaignLocalDaemonProcess$", lane.GoTestPattern, lane.GoTestPattern == "^TestCampaignLocalDaemonProcess$", "")
	})

	t.Run("process-backed-daemon-attach-use-stop", func(t *testing.T) {
		api := httptest.NewServer(newRestFixtureHandler(newFixtureStore()))
		t.Cleanup(api.Close)

		root := t.TempDir()
		repoRoot := filepath.Clean(filepath.Join(mustGetwd(t), "..", ".."))
		openapiPath := writeFile(t, root, "daemon-process.openapi.yaml", restOpenAPIYAML(api.URL))
		configPath := writeFile(t, root, ".cli.json", daemonProcessCLIConfig(openapiPath))
		stateDir := filepath.Join(root, "state")
		binDir := filepath.Join(root, "bin")
		ocliBin := buildBinary(t, repoRoot, "./cmd/ocli", filepath.Join(binDir, "ocli"))
		oclirdBin := buildBinary(t, repoRoot, "./cmd/oclird", filepath.Join(binDir, "oclird"))

		runtimeInfo := startDaemonProcess(t, repoRoot, oclirdBin, configPath, stateDir)
		commandEnv := append(os.Environ(), "OCLI_TERMINAL_SESSION_ID=process-lane-terminal")

		catalogOutput := runCommand(t, repoRoot, commandEnv, ocliBin,
			"--runtime", runtimeInfo.URL,
			"--config", configPath,
			"--state-dir", stateDir,
			"--instance-id", "process-lane",
			"--format", "json",
			"catalog", "list",
		)
		fr.CheckBool("process-catalog-output", "real ocli can attach to the daemon and fetch catalog output", strings.Contains(catalogOutput, `"catalog"`), catalogOutput)

		listOutput := runCommand(t, repoRoot, commandEnv, ocliBin,
			"--runtime", runtimeInfo.URL,
			"--config", configPath,
			"--state-dir", stateDir,
			"--instance-id", "process-lane",
			"--format", "json",
			"testapi", "items", "list-items",
		)
		fr.CheckBool("process-tool-output", "real ocli can execute a discovered tool through the daemon", strings.Contains(listOutput, `"items"`), listOutput)

		inst := &helpers.Instance{URL: runtimeInfo.URL, AuditPath: runtimeInfo.AuditPath}
		events := inst.AuditEvents(t)
		fr.Check("process-audit-events", "daemon audit log records the process-backed tool call", ">=1", fmt.Sprintf("%d", len(events)), len(events) >= 1, "")
		fr.CheckBool("process-audit-tool-id", "daemon audit log contains the listItems tool execution", hasAuditTool(events, "testapi:listItems"), "")

		stopOutput := runCommand(t, repoRoot, commandEnv, ocliBin,
			"--runtime", runtimeInfo.URL,
			"--config", configPath,
			"--state-dir", stateDir,
			"--instance-id", "process-lane",
			"--format", "json",
			"runtime", "stop",
		)
		fr.CheckBool("process-stop-output", "real ocli can stop the daemon cleanly", strings.Contains(stopOutput, `"stopped"`) || strings.Contains(stopOutput, `"ok"`), stopOutput)
		waitForDaemonExit(t, runtimeInfo)
		if _, err := os.Stat(runtimeInfo.RuntimePath); !os.IsNotExist(err) {
			t.Fatalf("expected runtime registry to be removed after daemon stop, stat err=%v", err)
		}
	})
}

func daemonProcessCLIConfig(openapiPath string) string {
	return fmt.Sprintf(`{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "runtime": {
    "mode": "local",
    "local": {
      "sessionScope": "shared-group",
      "heartbeatSeconds": 15,
      "missedHeartbeatLimit": 3,
      "shutdown": "manual",
      "share": "group",
      "shareKey": "process-lane"
    }
  },
  "sources": {
    "testapiSource": {
      "type": "openapi",
      "uri": %q,
      "enabled": true
    }
  },
  "services": {
    "testapi": {
      "source": "testapiSource",
      "alias": "testapi"
    }
  }
}`, openapiPath)
}

type daemonRuntime struct {
	URL         string
	AuditPath   string
	RuntimePath string
	process     *os.Process
	waitDone    <-chan error
}

func startDaemonProcess(t *testing.T, repoRoot, binary, configPath, stateDir string) daemonRuntime {
	t.Helper()

	paths, err := instance.Resolve(instance.Options{
		InstanceID: "process-lane",
		ConfigPath: configPath,
		StateRoot:  stateDir,
		CacheRoot:  filepath.Join(stateDir, "cache"),
	})
	if err != nil {
		t.Fatalf("resolve instance paths: %v", err)
	}

	cmd := exec.Command(binary,
		"--config", configPath,
		"--state-dir", stateDir,
		"--instance-id", "process-lane",
		"--heartbeat-seconds", "15",
		"--missed-heartbeat-limit", "3",
		"--shutdown", "manual",
		"--session-scope", "shared-group",
		"--share", "group",
		"--share-key-present", "true",
	)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start oclird: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	t.Cleanup(func() {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return
		}
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := instance.ReadRuntimeInfo(paths.RuntimePath)
		if err == nil && info.URL != "" {
			return daemonRuntime{
				URL:         info.URL,
				AuditPath:   paths.AuditPath,
				RuntimePath: paths.RuntimePath,
				process:     cmd.Process,
				waitDone:    waitDone,
			}
		}
		select {
		case err := <-waitDone:
			t.Fatalf("oclird exited before becoming ready: %v stderr=%s", err, stderr.String())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("oclird did not publish runtime info within deadline; stderr=%s", stderr.String())
	return daemonRuntime{}
}

func waitForDaemonExit(t *testing.T, runtime daemonRuntime) {
	t.Helper()

	select {
	case err := <-runtime.waitDone:
		if err != nil {
			t.Fatalf("oclird exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("oclird did not exit after runtime stop")
	}
}

func buildBinary(t *testing.T, repoRoot, pkg, output string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(output), err)
	}
	cmd := exec.Command("go", "build", "-o", output, pkg)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build %s: %v stderr=%s", pkg, err, stderr.String())
	}
	return output
}

func runCommand(t *testing.T, dir string, env []string, binary string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v stderr=%s", binary, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

func mustGetwd(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

func findCapabilityLane(matrix *helpers.CapabilityMatrix, id string) *helpers.CapabilityLane {
	for i := range matrix.Lanes {
		if matrix.Lanes[i].ID == id {
			return &matrix.Lanes[i]
		}
	}
	return nil
}

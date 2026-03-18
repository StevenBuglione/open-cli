package tests_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignLocalDaemonMatrix(t *testing.T) {
	fr := helpers.NewFindingsRecorder("local-daemon-matrix")
	fr.SetLaneMetadata("product-validation", "local-daemon", "ci-containerized", "none")
	defer fr.MustEmitToTest(t)

	t.Run("exclusive-attach-conflict", func(t *testing.T) {
		inst := helpers.NewLifecycleInstance(t, runtime.Options{
			HeartbeatSeconds:     15,
			MissedHeartbeatLimit: 3,
			ShutdownMode:         "when-owner-exits",
			SessionScope:         "terminal",
			ShareMode:            "exclusive",
			ConfigFingerprint:    "fp-1",
		})

		status, _, body := inst.Heartbeat(t, "sess-1", "fp-1")
		fr.Check("exclusive-first-attach", "first exclusive heartbeat succeeds", "200", fmt.Sprintf("%d", status), status == http.StatusOK, body)

		status, _, body = inst.Heartbeat(t, "sess-2", "fp-1")
		fr.Check("exclusive-second-attach-conflict", "second exclusive heartbeat is rejected", "409/runtime_attach_conflict", fmt.Sprintf("%d/%s", status, body), status == http.StatusConflict && body == "runtime_attach_conflict", "")
	})

	t.Run("shared-group-allows-multiple-sessions", func(t *testing.T) {
		inst := helpers.NewLifecycleInstance(t, runtime.Options{
			HeartbeatSeconds:     15,
			MissedHeartbeatLimit: 3,
			ShutdownMode:         "manual",
			SessionScope:         "shared-group",
			ShareMode:            "group",
			ConfigFingerprint:    "fp-1",
		})

		status, payload, body := inst.Heartbeat(t, "sess-1", "fp-1")
		fr.Check("shared-first-attach", "first shared-group heartbeat succeeds", "200", fmt.Sprintf("%d", status), status == http.StatusOK, body)

		status, payload, body = inst.Heartbeat(t, "sess-2", "fp-1")
		activeSessions, _ := payload["activeSessions"].(float64)
		fr.Check("shared-second-attach", "second shared-group heartbeat also succeeds", "200", fmt.Sprintf("%d", status), status == http.StatusOK, body)
		fr.Check("shared-active-sessions", "shared-group runtime tracks both sessions", "2", fmt.Sprintf("%.0f", activeSessions), activeSessions == 2, "")
	})

	t.Run("manual-retention-after-expiry", func(t *testing.T) {
		inst := helpers.NewLifecycleInstance(t, runtime.Options{
			HeartbeatSeconds:     1,
			MissedHeartbeatLimit: 1,
			ShutdownMode:         "manual",
			SessionScope:         "shared-group",
			ShareMode:            "group",
			ConfigFingerprint:    "fp-1",
		})

		status, _, body := inst.Heartbeat(t, "sess-1", "fp-1")
		fr.Check("manual-retention-heartbeat", "manual-retention heartbeat succeeds", "200", fmt.Sprintf("%d", status), status == http.StatusOK, body)

		select {
		case <-inst.ShutdownSignal:
			fr.CheckBool("manual-retention-expiry", "manual runtime stays alive after lease expiry", false, "runtime emitted shutdown signal")
		case <-time.After(1500 * time.Millisecond):
			fr.CheckBool("manual-retention-expiry", "manual runtime stays alive after lease expiry", true, "")
		}
	})
}

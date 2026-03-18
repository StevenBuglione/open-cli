package runtime_test

import (
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
)

// fastLeaseConfig returns a LeaseConfig tuned for fast tests.
// interval=20ms, limit=2 → stale after 40ms; grace=30ms.
func fastLeaseConfig() runtime.LeaseConfig {
	return runtime.LeaseConfig{
		HeartbeatInterval:    20 * time.Millisecond,
		MissedHeartbeatLimit: 2,
		GracePeriod:          30 * time.Millisecond,
	}
}

// TestLeaseTrackerHeartbeatRenewsPreventsShutdown confirms that sending
// heartbeats keeps the lease alive beyond its natural expiry window.
func TestLeaseTrackerHeartbeatRenewsPreventsShutdown(t *testing.T) {
	shutdownCalled := false
	lt := runtime.NewLeaseTracker(fastLeaseConfig(), func() { shutdownCalled = true })

	stop := make(chan struct{})
	defer close(stop)
	lt.Start(stop)

	// Send heartbeats frequently — more often than the stale threshold.
	deadline := time.Now().Add(90 * time.Millisecond)
	for time.Now().Before(deadline) {
		lt.Heartbeat()
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-lt.Done():
		t.Fatal("shutdown triggered while heartbeats were active")
	default:
	}
	if shutdownCalled {
		t.Fatal("onShutdown called while heartbeats were active")
	}
}

// TestLeaseTrackerExpiresWithNoInflightImmediateShutdown confirms that when no
// request is in-flight and heartbeats stop, shutdown is triggered promptly.
func TestLeaseTrackerExpiresWithNoInflightImmediateShutdown(t *testing.T) {
	lt := runtime.NewLeaseTracker(fastLeaseConfig(), nil)

	stop := make(chan struct{})
	defer close(stop)
	lt.Start(stop)

	// Do not call Heartbeat — let the lease go stale.
	select {
	case <-lt.Done():
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown not triggered after lease expired with no in-flight request")
	}
}

// TestLeaseTrackerExpiresWithInflightWaitsForCompletion is the core grace
// behaviour: a slow request that starts just before lease expiry should be
// allowed to finish before shutdown fires.
func TestLeaseTrackerExpiresWithInflightWaitsForCompletion(t *testing.T) {
	var shutdownAt time.Time
	lt := runtime.NewLeaseTracker(fastLeaseConfig(), func() { shutdownAt = time.Now() })

	stop := make(chan struct{})
	defer close(stop)
	lt.Start(stop)

	// Simulate a slow request: acquire inflight, sleep 25 ms, release.
	requestDone := make(chan struct{})
	var requestEndedAt time.Time
	go func() {
		lt.AcquireInflight()
		defer close(requestDone)
		time.Sleep(25 * time.Millisecond)
		requestEndedAt = time.Now()
		lt.ReleaseInflight()
	}()

	// Wait for shutdown (grace period covers the request duration).
	select {
	case <-lt.Done():
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown never triggered after in-flight grace period")
	}

	<-requestDone // ensure goroutine finished

	if shutdownAt.Before(requestEndedAt) || shutdownAt.Equal(time.Time{}) {
		t.Fatalf("shutdown triggered before in-flight request finished: shutdown=%v request=%v", shutdownAt, requestEndedAt)
	}
}

// TestLeaseTrackerExpiresWithInflightForcesShutdownAfterGrace verifies that
// shutdown fires after GracePeriod even when the in-flight request has not
// completed.
func TestLeaseTrackerExpiresWithInflightForcesShutdownAfterGrace(t *testing.T) {
	cfg := fastLeaseConfig()
	cfg.GracePeriod = 15 * time.Millisecond

	lt := runtime.NewLeaseTracker(cfg, nil)

	stop := make(chan struct{})
	defer close(stop)
	lt.Start(stop)

	// Hold a request open longer than the grace period to trigger forced shutdown.
	lt.AcquireInflight()

	select {
	case <-lt.Done():
		// expected: forced shutdown after grace
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown not triggered after grace period with stuck in-flight request")
	}

	lt.ReleaseInflight()
}

// TestLeaseTrackerCloseTriggersImmediateShutdown verifies that an explicit
// Close call triggers shutdown regardless of heartbeat state.
func TestLeaseTrackerCloseTriggersImmediateShutdown(t *testing.T) {
	lt := runtime.NewLeaseTracker(fastLeaseConfig(), nil)

	stop := make(chan struct{})
	defer close(stop)
	lt.Start(stop)

	lt.Heartbeat()
	lt.Close()

	select {
	case <-lt.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close() did not trigger shutdown")
	}
}

// TestLeaseTrackerCloseTwiceIsSafe verifies idempotency.
func TestLeaseTrackerCloseTwiceIsSafe(t *testing.T) {
	lt := runtime.NewLeaseTracker(fastLeaseConfig(), nil)
	lt.Close()
	lt.Close() // must not panic
}

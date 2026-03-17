package helpers

import "testing"

func TestResolveAuthentikFixtureAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		containerID string
		enableTests bool
		wantReady   bool
		wantFatal   bool
		wantMessage string
	}{
		{
			name:        "skip when live authentik tests are not enabled",
			containerID: "",
			enableTests: false,
			wantReady:   false,
			wantFatal:   false,
			wantMessage: "live Authentik product tests are disabled; set OASCLI_RUN_AUTHENTIK_TESTS=1 or use make test-runtime-auth-authentik",
		},
		{
			name:        "fail when stack is required but absent",
			containerID: "",
			enableTests: true,
			wantReady:   false,
			wantFatal:   true,
			wantMessage: "worker container is not running; start the Authentik stack with product-tests/scripts/authentik-up.sh or make authentik-up",
		},
		{
			name:        "run when worker container exists",
			containerID: "container-123",
			enableTests: true,
			wantReady:   true,
			wantFatal:   false,
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotReady, gotFatal, gotMessage := resolveAuthentikFixtureAvailability("worker", tt.containerID, tt.enableTests)
			if gotReady != tt.wantReady {
				t.Fatalf("expected ready=%v, got %v", tt.wantReady, gotReady)
			}
			if gotFatal != tt.wantFatal {
				t.Fatalf("expected fatal=%v, got %v", tt.wantFatal, gotFatal)
			}
			if gotMessage != tt.wantMessage {
				t.Fatalf("expected message %q, got %q", tt.wantMessage, gotMessage)
			}
		})
	}
}

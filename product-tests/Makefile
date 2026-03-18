.PHONY: check-prereqs services-up services-down authentik-up authentik-down mcp-remote-up mcp-remote-down smoke full test-mcp-stdio test-mcp-remote test-runtime-auth-authentik fleet-matrix-ci fleet-matrix-mcp-remote test-website-review

FLEET_OUTPUT_DIR ?= /tmp/oascli-fleet
FLEET_MATRIX ?= product-tests/testdata/fleet/capability-matrix.yaml

define summarize_fleet
python3 scripts/summarize-findings.py $$(find $(1) \( -name rubric.json -o -name '*.rubric.json' \) -type f | sort)
endef

check-prereqs:
	@bash scripts/check-prereqs.sh

services-up: check-prereqs
	@bash scripts/services-up.sh

services-down:
	@bash scripts/services-down.sh

authentik-up: check-prereqs
	@bash scripts/authentik-up.sh

authentik-down:
	@bash scripts/authentik-down.sh

mcp-remote-up:
	docker compose -f mcp/remote/docker-compose.yml up -d

mcp-remote-down:
	docker compose -f mcp/remote/docker-compose.yml down

test-mcp-stdio:
	go test ./tests/... -run TestCapabilityExecuteMCPStdio -count=1 -v -timeout 180s

test-mcp-remote: mcp-remote-up
	@echo "==> Waiting for remote MCP service..."
	@sleep 8
	go test ./tests/... -run TestCapabilityExecuteMCPRemote -count=1 -v -timeout 60s; \
	  $(MAKE) mcp-remote-down; \
	  exit $$?

test-runtime-auth-authentik: authentik-up
	@echo "==> Waiting for Authentik reference stack..."
	@sleep 10
	OCLI_RUN_AUTHENTIK_TESTS=1 go test ./tests/... -run TestCapabilityRuntimeAuthAuthentik -count=1 -v -timeout 180s; \
	  status=$$?; \
	  $(MAKE) authentik-down; \
	  exit $$status

test-website-review:
	go test ./tests/... -run '^TestCampaignWebsiteReview$$' -count=1 -v -timeout 60s

fleet-matrix-ci:
	rm -rf $(FLEET_OUTPUT_DIR)
	python3 scripts/run-agent-campaign.py --matrix $(FLEET_MATRIX) \
	  --lane local-daemon-core \
	  --lane local-daemon-process \
	  --lane remote-runtime-oauth-client \
	  --lane remote-runtime-auth-failures \
	  --lane mcp-stdio-core \
	  --lane remote-api-core \
	  --lane remote-api-failures \
	  --lane remote-api-pagination \
	  --lane remote-api-concurrency \
	  --lane remote-api-nonjson \
	  --output-dir $(FLEET_OUTPUT_DIR)
	$(call summarize_fleet,$(FLEET_OUTPUT_DIR))

fleet-matrix-mcp-remote: mcp-remote-up
	@bash scripts/wait-for-mcp-remote.sh
	rm -rf $(FLEET_OUTPUT_DIR)/mcp-remote-core
	python3 scripts/run-agent-campaign.py --matrix $(FLEET_MATRIX) \
	  --lane mcp-remote-core \
	  --output-dir $(FLEET_OUTPUT_DIR); \
	  status=$$?; \
	  $(call summarize_fleet,$(FLEET_OUTPUT_DIR)/mcp-remote-core); \
	  $(MAKE) mcp-remote-down; \
	  exit $$status

smoke: check-prereqs
	@echo "==> Validating compose configs..."
	docker compose config --quiet
	docker compose -f authentik/docker-compose.yml config --quiet
	docker compose -f mcp/remote/docker-compose.yml config --quiet
	@echo "==> Smoke OK (infrastructure configs valid)"

full: smoke services-up
	@echo "==> Full suite placeholder — add test targets here"
	$(MAKE) services-down

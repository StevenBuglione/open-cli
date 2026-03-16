.PHONY: check-prereqs services-up services-down authentik-up authentik-down mcp-remote-up mcp-remote-down smoke full test-mcp-stdio test-mcp-remote test-runtime-auth-authentik

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
	go test ./tests/... -run TestCapabilityRuntimeAuthAuthentik -count=1 -v -timeout 180s; \
	  status=$$?; \
	  $(MAKE) authentik-down; \
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

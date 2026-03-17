.PHONY: fmt test build verify verify-spec verify-conformance verify-all product-test-smoke product-test-full product-test-fleet product-test-fleet-mcp-remote

fmt:
	gofmt -w $$(find . -name '*.go' -print)

test:
	go test ./...

build:
	go build ./cmd/oascli ./cmd/oasclird

verify: fmt test build

verify-spec:
	cd spec && python3 -m pip install -q -r requirements.txt && python3 scripts/validate_examples.py

verify-conformance:
	cd conformance && python3 -m pip install -q -r requirements.txt && python3 scripts/run_conformance.py --schema-root ../spec/schemas

verify-all: verify verify-spec verify-conformance

product-test-smoke:
	cd product-tests && $(MAKE) smoke

product-test-full:
	cd product-tests && $(MAKE) full

product-test-fleet:
	cd product-tests && $(MAKE) fleet-matrix-ci

product-test-fleet-mcp-remote:
	cd product-tests && $(MAKE) fleet-matrix-mcp-remote

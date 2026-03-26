.PHONY: fmt test build verify verify-spec verify-conformance verify-all product-test-smoke product-test-full product-test-fleet product-test-fleet-mcp-remote product-test-website-review

fmt:
	gofmt -w $$(find . -name '*.go' -print)

test:
	go test ./...

build:
	go build ./cmd/ocli ./cmd/open-cli-toolbox

verify: fmt test build

verify-spec:
	python3 devtools/bootstrap_python_env.py --requirements spec/requirements.txt -- .venv/bin/python3 spec/scripts/validate_examples.py

verify-conformance:
	python3 devtools/bootstrap_python_env.py --requirements conformance/requirements.txt -- .venv/bin/python3 conformance/scripts/run_conformance.py --schema-root spec/schemas

verify-all: verify verify-spec verify-conformance

product-test-smoke:
	cd product-tests && $(MAKE) smoke

product-test-full:
	cd product-tests && $(MAKE) full

product-test-fleet:
	cd product-tests && $(MAKE) fleet-matrix-ci

product-test-fleet-mcp-remote:
	cd product-tests && $(MAKE) fleet-matrix-mcp-remote

product-test-website-review:
	cd product-tests && $(MAKE) test-website-review

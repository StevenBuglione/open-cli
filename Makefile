.PHONY: fmt test build verify verify-spec verify-conformance verify-all

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

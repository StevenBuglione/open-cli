---
title: Installation
---

# Installation

**This repository uses source-based installation.** There are no packaged installers or pre-built release binaries — you compile from source using Go.

**Time to usable binaries: ~1 minute** if you already have Go installed.

## Prerequisites

- **Go 1.25.1 or newer** — the version in `go.mod` is the authoritative requirement.
- Git (to clone the repository).
- **Node.js 18+** only if you need to build the docs site (`website/`).

## Fastest path: build and go

From the repository root, run:

```bash
go build -o ./bin/ocli ./cmd/ocli
go build -o ./bin/oclird ./cmd/oclird
```

That produces `./bin/ocli` and `./bin/oclird`. **If you only need binaries to follow the quickstart, stop here** and continue to [Quickstart](./quickstart).

## Alternative: install into your Go bin directory

```bash
go install ./cmd/ocli
go install ./cmd/oclird
```

This puts the binaries on your `$PATH` via `$GOPATH/bin`. Use this if you want to run `ocli` without the `./bin/` prefix.

## Alternative: run directly from source (no build step)

```bash
go run ./cmd/oclird --help
go run ./cmd/ocli --embedded --config /path/to/.cli.json catalog list
```

Useful for a one-off check. Slower than a compiled binary because Go compiles on each invocation.

## What to do after building

1. **Go to [Quickstart](./quickstart)** — it walks you through creating a minimal config and running your first embedded-mode command.
2. Once that works, return to [Choose your path](./choose-your-path) to pick the runtime model that fits your workload.

## Verify the full checkout (optional)

```bash
make verify
```

This runs `gofmt`, `go test ./...`, and `go build` on the entire codebase. Run this before submitting changes. If you are only following the quickstart, you do not need it.

## Docs site contributors

```bash
cd website
npm install
npm run build
```

The docs site uses the existing `website/package.json` toolchain; no additional site tooling is needed.

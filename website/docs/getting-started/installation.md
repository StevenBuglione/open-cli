---
title: Installation
---

# Installation

This repository currently documents **source-based installation**. There are no packaged installers or release-specific install commands in the codebase.

## Prerequisites

- **Go 1.25.1 or newer**. The version in `go.mod` is the authoritative requirement for this worktree.
- **Node.js 18 or newer** only if you need to build the docs site under `website/`.

## Build the binaries

From the repository root:

```bash
go build -o ./bin/oascli ./cmd/oascli
go build -o ./bin/oasclird ./cmd/oasclird
```

Or install them into your Go bin directory:

```bash
go install ./cmd/oascli
go install ./cmd/oasclird
```

If you prefer not to install anything yet, you can also run both binaries directly from source:

```bash
go run ./cmd/oasclird --help
go run ./cmd/oascli --embedded --config /path/to/.cli.json catalog list
```

## Verify the checkout

The repository-level verification target is:

```bash
make verify
```

Today that expands to:

- `gofmt -w $(find . -name '*.go' -print)`
- `go test ./...`
- `go build ./cmd/oascli ./cmd/oasclird`

If you are only changing docs, you usually just need the docs build under `website/`, but `make verify` is still the best baseline when touching Go code.

## First-run checklist

After building:

1. Create a `.cli.json` config file.
2. Decide whether you want **embedded mode** (`oascli --embedded`) or a long-running **daemon** (`oasclird`).
3. Run `catalog list` first to confirm that discovery and normalization work before you try tool execution.

## Docs site contributors

To work on the Docusaurus site:

```bash
cd website
npm install
npm run build
```

The current docs build requires the existing `website/package.json` toolchain; no extra site-specific tooling is implemented in this repo.

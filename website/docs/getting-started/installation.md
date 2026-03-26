---
title: Installation
---

# Installation

`open-cli` ships two binaries — `ocli` (the client) and `open-cli-toolbox` (the hosted runtime). The supported model is remote-only: `ocli` always talks to a reachable runtime.

## npm (Recommended)

```bash
npm install -g @sbuglione/open-cli
```

The package automatically downloads the correct pre-built binaries for your platform during `postinstall`. After installation, both `ocli` and `open-cli-toolbox` are available on your `PATH`.

## Binary Download

Pre-built binaries for every supported platform are attached to each [GitHub Release](https://github.com/StevenBuglione/open-cli/releases).

1. Download the archive for your OS and architecture.
2. Extract it (`tar xzf` on macOS/Linux, or unzip on Windows).
3. Move `ocli` and `open-cli-toolbox` to a directory on your `PATH`.

**macOS / Linux:**

```bash
tar xzf open-cli_<os>_<arch>.tar.gz
sudo mv ocli open-cli-toolbox /usr/local/bin/
```

**Windows:**

Extract the `.zip` archive and add the folder containing `ocli.exe` and `open-cli-toolbox.exe` to your system `PATH`.

## From Source

Requires **Go 1.25.1+**.

**Install into your Go bin directory:**

```bash
go install github.com/StevenBuglione/open-cli/cmd/ocli@latest
go install github.com/StevenBuglione/open-cli/cmd/open-cli-toolbox@latest
```

**Or build from a local clone (for contributors):**

```bash
git clone https://github.com/StevenBuglione/open-cli.git
cd open-cli
go build -o ./bin/ocli ./cmd/ocli
go build -o ./bin/open-cli-toolbox ./cmd/open-cli-toolbox
```

## Verify Installation

```bash
ocli --version
open-cli-toolbox --help
```

If those commands work, continue to [Quickstart](./quickstart).

## Platform Support

| OS | x64 | arm64 |
|----|-----|-------|
| macOS | ✅ | ✅ |
| Linux | ✅ | ✅ |
| Windows | ✅ | ✅ |

## Troubleshooting

- **`npm install` fails behind a proxy** — set `HTTPS_PROXY` before installing. The postinstall script follows standard proxy environment variables.
- **Permission denied on global install** — use `sudo npm install -g @sbuglione/open-cli` or configure npm to use a user-writable prefix (`npm config set prefix ~/.npm-global`).
- **Binary not found after install** — ensure your npm global `bin` directory is on your `PATH` (`npm bin -g`).
- **Go build fails** — verify your Go version with `go version`; the minimum required is Go 1.25.1.

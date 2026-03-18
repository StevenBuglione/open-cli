---
title: Installation
---

# Installation

open-cli ships two binaries — `ocli` (the CLI) and `oclird` (the runtime daemon). Pick the installation method that fits your environment.

## npm (Recommended)

```bash
npm install -g @sbuglione/open-cli
```

The package automatically downloads the correct pre-built binary for your platform during `postinstall`. After installation, both `ocli` and `oclird` are available on your `PATH`.

## Binary Download

Pre-built binaries for every supported platform are attached to each [GitHub Release](https://github.com/StevenBuglione/open-cli/releases).

1. Download the archive for your OS and architecture.
2. Extract it (`tar xzf` on macOS/Linux, or unzip on Windows).
3. Move `ocli` and `oclird` to a directory on your `PATH`.

**macOS / Linux:**

```bash
tar xzf open-cli_<os>_<arch>.tar.gz
sudo mv ocli oclird /usr/local/bin/
```

**Windows:**

Extract the `.zip` archive and add the folder containing `ocli.exe` and `oclird.exe` to your system `PATH`.

## From Source

Requires **Go 1.25.1+**.

**Install into your Go bin directory:**

```bash
go install github.com/StevenBuglione/open-cli/cmd/ocli@latest
go install github.com/StevenBuglione/open-cli/cmd/oclird@latest
```

**Or build from a local clone (for contributors):**

```bash
git clone https://github.com/StevenBuglione/open-cli.git
cd open-cli
go build -o ./bin/ocli ./cmd/ocli
go build -o ./bin/oclird ./cmd/oclird
```

## Verify Installation

```bash
ocli --version
```

If the version prints, you are ready. Continue to [Quickstart](./quickstart).

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

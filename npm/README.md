# @sbuglione/open-cli

> Remote-only API and MCP command tooling with an operator-hosted open-cli-toolbox runtime server.

## What is open-cli?

**open-cli** (`ocli`) converts OpenAPI specs and MCP server definitions into executable CLI commands, so you can explore, test, and audit APIs from your terminal. `ocli` always connects to a separately hosted runtime server (`open-cli-toolbox`), which handles catalog execution, token-scoped tool exposure, policy evaluation, and enterprise deployment concerns.

## Platform Support

| OS      | x64 | arm64 |
|---------|-----|-------|
| macOS   | ✓   | ✓     |
| Linux   | ✓   | ✓     |
| Windows | ✓   | ✓     |

## Installation

```bash
npm install -g @sbuglione/open-cli
```

### What happens during install

The `postinstall` script automatically downloads the correct pre-built `ocli` and `open-cli-toolbox` binaries for your platform from [GitHub Releases](https://github.com/StevenBuglione/open-cli/releases). No compiler or Go toolchain is needed.

## Quick Start

```bash
# 1. Install globally
npm install -g @sbuglione/open-cli

# 2. Point ocli at your hosted runtime
ocli --runtime https://toolbox.example.com

# 3. Initialize your own API catalog
ocli init <your-api>
```

`ocli` does not embed a local execution daemon. Operators host `open-cli-toolbox`, secure it, and decide which tools are visible to each token.

## Troubleshooting

| Problem | Cause | Solution |
|---------|-------|----------|
| `binary not found at …` | `postinstall` didn't run or failed | Run `npm install -g @sbuglione/open-cli` again |
| `HTTP 404` during install | Version/platform mismatch or unpublished release | Check [releases](https://github.com/StevenBuglione/open-cli/releases) for your platform |
| SSL / proxy errors | Corporate proxy or firewall blocking GitHub | Set `https_proxy` env var or download the binary manually |
| Download timeout | Slow connection | Set `OPEN_CLI_DOWNLOAD_TIMEOUT=120` (seconds) before install |
| `permission denied` | Binary lacks execute permission | Run `chmod +x $(npm prefix -g)/lib/node_modules/@sbuglione/open-cli/bin/ocli` |

## Configuration

See the full documentation at [https://open-cli.dev/](https://open-cli.dev/) for configuration options, policy files, and advanced usage.

## License

[GPL-3.0](https://github.com/StevenBuglione/open-cli/blob/main/LICENSE)

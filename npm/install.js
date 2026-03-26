#!/usr/bin/env node

"use strict";

const os = require("os");
const fs = require("fs");
const path = require("path");
const https = require("https");
const { execSync, execFileSync } = require("child_process");

const REPO = "StevenBuglione/open-cli";
const BINARIES = ["ocli", "open-cli-toolbox"];
const MAX_RETRIES = 3;
const DEFAULT_TIMEOUT_S = 30;

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

// --- Signal-safe temp directory cleanup ---
let tempDir = null;
const cleanup = () => {
  if (tempDir) {
    try { fs.rmSync(tempDir, { recursive: true, force: true }); } catch {}
    tempDir = null;
  }
};
process.on("SIGINT", () => { cleanup(); process.exit(130); });
process.on("SIGTERM", () => { cleanup(); process.exit(143); });

function getVersion() {
  return require("./package.json").version;
}

function getPlatformArch() {
  const platform = PLATFORM_MAP[os.platform()];
  const arch = ARCH_MAP[os.arch()];

  if (!platform) {
    console.error(
      `open-cli: unsupported platform: ${os.platform()}. Supported: ${Object.keys(PLATFORM_MAP).join(", ")}`
    );
    process.exit(1);
  }
  if (!arch) {
    console.error(
      `open-cli: unsupported architecture: ${os.arch()}. Supported: ${Object.keys(ARCH_MAP).join(", ")}`
    );
    process.exit(1);
  }

  return { platform, arch };
}

function getArchiveName(version, platform, arch) {
  const ext = platform === "windows" ? "zip" : "tar.gz";
  return `open-cli_${version}_${platform}_${arch}.${ext}`;
}

function getDownloadUrl(version, archiveName) {
  return `https://github.com/${REPO}/releases/download/v${version}/${archiveName}`;
}

function getTimeoutMs() {
  const s = parseInt(process.env.OPEN_CLI_DOWNLOAD_TIMEOUT || String(DEFAULT_TIMEOUT_S), 10);
  return (Number.isFinite(s) && s > 0 ? s : DEFAULT_TIMEOUT_S) * 1000;
}

function download(url, timeoutMs, maxRedirects = 5) {
  return new Promise((resolve, reject) => {
    if (maxRedirects <= 0) {
      return reject(new Error("Too many redirects"));
    }

    const proto = url.startsWith("https") ? https : require("http");
    const req = proto.get(
      url,
      { headers: { "User-Agent": "open-cli-npm-installer" } },
      (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return resolve(download(res.headers.location, timeoutMs, maxRedirects - 1));
        }
        if (res.statusCode !== 200) {
          return reject(
            new Error(`HTTP ${res.statusCode} for ${url}`)
          );
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      }
    );

    req.on("error", reject);
    req.setTimeout(timeoutMs, () => {
      req.destroy(new Error(`download timed out after ${timeoutMs / 1000}s`));
    });
  });
}

async function downloadWithRetry(url) {
  const timeoutMs = getTimeoutMs();
  for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
    try {
      return await download(url, timeoutMs);
    } catch (err) {
      if (attempt === MAX_RETRIES) throw err;
      const delay = Math.pow(2, attempt - 1) * 1000;
      console.error(
        `open-cli: attempt ${attempt}/${MAX_RETRIES} failed: ${err.message}. Retrying in ${delay / 1000}s...`
      );
      await new Promise((r) => setTimeout(r, delay));
    }
  }
}

async function extractArchive(buffer, platform, binDir) {
  tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "open-cli-"));

  if (platform === "windows") {
    const zipPath = path.join(tempDir, "archive.zip");
    fs.writeFileSync(zipPath, buffer);
    try {
      execSync(`tar -xf "${zipPath}" -C "${tempDir}"`, { stdio: "pipe" });
    } catch {
      execSync(
        `powershell -Command "Expand-Archive -Path '${zipPath}' -DestinationPath '${tempDir}'"`,
        { stdio: "pipe" }
      );
    }
  } else {
    const tarPath = path.join(tempDir, "archive.tar.gz");
    fs.writeFileSync(tarPath, buffer);
    execSync(`tar -xzf "${tarPath}" -C "${tempDir}"`, { stdio: "pipe" });
  }

  const ext = platform === "windows" ? ".exe" : "";
  for (const binary of BINARIES) {
    const srcName = binary + ext;
    const src = path.join(tempDir, srcName);

    if (fs.existsSync(src)) {
      const dest = path.join(binDir, srcName);
      fs.copyFileSync(src, dest);
      if (platform !== "windows") {
        fs.chmodSync(dest, 0o755);
      }
    }
  }

  cleanup();
}

function validateBinaries(binDir, ext) {
  for (const name of BINARIES) {
    const bin = path.join(binDir, name + ext);
    if (!fs.existsSync(bin)) {
      console.error(`open-cli: binary "${name}" missing after extraction`);
      process.exit(1);
    }
  }
}

function verifyVersion(binDir, ext, platform, arch) {
  try {
    const out = execFileSync(path.join(binDir, "ocli" + ext), ["--version"], {
      timeout: 5000,
    }).toString().trim();
    console.log(`open-cli: ✓ installed ocli and open-cli-toolbox (${out}, ${platform}-${arch})`);
  } catch {
    console.error("open-cli: ⚠ binaries downloaded but --version check failed");
  }
}

async function main() {
  const version = getVersion();
  const { platform, arch } = getPlatformArch();
  const archiveName = getArchiveName(version, platform, arch);
  const url = getDownloadUrl(version, archiveName);
  const binDir = path.join(__dirname, "bin");
  const ext = platform === "windows" ? ".exe" : "";

  // Skip download if binaries already present (e.g. from CI)
  const allPresent = BINARIES.every((b) =>
    fs.existsSync(path.join(binDir, b + ext))
  );
  if (allPresent) {
    console.log("open-cli: binaries already present, skipping download");
    return;
  }

  console.log(`open-cli: downloading v${version} for ${platform}-${arch}...`);

  try {
    const buffer = await downloadWithRetry(url);
    await extractArchive(buffer, platform, binDir);
    validateBinaries(binDir, ext);
    verifyVersion(binDir, ext, platform, arch);
    console.log("open-cli: run `ocli --help` to get started");
  } catch (err) {
    cleanup();
    console.error(`open-cli: failed to install — ${err.message}`);
    console.error(
      `open-cli: you can download manually from https://github.com/${REPO}/releases`
    );
    process.exit(1);
  }
}

main();

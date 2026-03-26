#!/usr/bin/env node

"use strict";

const fs = require("fs");
const { execFileSync } = require("child_process");
const path = require("path");

const ext = process.platform === "win32" ? ".exe" : "";
const bin = path.join(__dirname, "open-cli-toolbox" + ext);

if (!fs.existsSync(bin)) {
  console.error(`open-cli: binary not found at ${bin}`);
  console.error(`open-cli: run "npm install -g @sbuglione/open-cli" to reinstall`);
  process.exit(1);
}

try {
  execFileSync(bin, process.argv.slice(2), {
    stdio: "inherit",
    windowsHide: true,
  });
} catch (e) {
  if (e.status !== null) {
    process.exit(e.status);
  }
  if (e.code === "EACCES") {
    console.error(`open-cli: permission denied on ${bin}`);
    console.error(`open-cli: try: chmod +x ${bin}`);
  } else {
    console.error(`open-cli: failed to execute ${bin}: ${e.message}`);
  }
  process.exit(1);
}

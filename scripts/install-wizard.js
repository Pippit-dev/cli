#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const DEFAULT_PKG = "@pippit-dev/cli";
const PKG = process.env.PIPPIT_CLI_INSTALL_PACKAGE || DEFAULT_PKG;
const isWindows = process.platform === "win32";

function runSilent(cmd, args, opts = {}) {
  return execFileSync(cmd, args, {
    stdio: ["ignore", "pipe", "pipe"],
    ...opts,
  });
}

function run(cmd, args, opts = {}) {
  execFileSync(cmd, args, { stdio: "inherit", ...opts });
}

function getGloballyInstalledVersion() {
  try {
    const out = runSilent("npm", ["list", "-g", DEFAULT_PKG], { timeout: 15000 });
    const match = out.toString().match(/@(\d+\.\d+\.\d+[^\s]*)/);
    return match ? match[1] : "unknown";
  } catch (_) {
    return null;
  }
}

function whichPippitCli() {
  try {
    const prefix = runSilent("npm", ["prefix", "-g"], { timeout: 15000 }).toString().trim();
    const bin = isWindows
      ? path.join(prefix, "pippit-cli.cmd")
      : path.join(prefix, "bin", "pippit-cli");
    if (fs.existsSync(bin)) return bin;
  } catch (_) {
    // Fall back to PATH lookup.
  }

  try {
    const cmd = isWindows ? "where" : "which";
    return runSilent(cmd, ["pippit-cli"]).toString().split("\n")[0].trim();
  } catch (_) {
    return null;
  }
}

function main() {
  const installed = getGloballyInstalledVersion();
  if (installed) {
    console.log(`pippit-cli is already installed globally (${installed}).`);
  } else {
    console.log(`Installing ${PKG} globally...`);
    run("npm", ["install", "-g", PKG], { timeout: 120000 });
  }

  const bin = whichPippitCli();
  if (!bin) {
    console.error("pippit-cli was installed, but no global command was found in npm prefix.");
    console.error("Check that npm's global bin directory is in PATH.");
    process.exit(1);
  }

  console.log(`pippit-cli is ready: ${bin}`);
  console.log("Try: pippit-cli novel +submit-run --prompt \"写一个小说开头\"");
}

main();

#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const { isWindows, run, runSilent } = require("./platform");
const { DEFAULT_PKG, installGlobalPackageSkills } = require("./skills");
const { reportBundledSkillTelemetry } = require("./telemetry");

const PKG = process.env.PIPPIT_CLI_INSTALL_PACKAGE || DEFAULT_PKG;

function getGloballyInstalledVersion() {
  try {
    const out = runSilent("npm", ["list", "-g", DEFAULT_PKG], { timeout: 15000 });
    const match = out.toString().match(/@(\d+\.\d+\.\d+[^\s]*)/);
    return match ? match[1] : "unknown";
  } catch (_) {
    return null;
  }
}

function whichPippitToolCli() {
  try {
    const prefix = runSilent("npm", ["prefix", "-g"], { timeout: 15000 }).toString().trim();
    const bin = isWindows
      ? path.join(prefix, "pippit-tool-cli.cmd")
      : path.join(prefix, "bin", "pippit-tool-cli");
    if (fs.existsSync(bin)) return bin;
  } catch (_) {
    // Fall back to PATH lookup.
  }

  try {
    const cmd = isWindows ? "where" : "which";
    return runSilent(cmd, ["pippit-tool-cli"]).toString().split("\n")[0].trim();
  } catch (_) {
    return null;
  }
}

function main() {
  const installed = getGloballyInstalledVersion();
  if (installed) {
    console.log(`Updating global pippit-tool-cli (${installed}) via ${PKG}...`);
  } else {
    console.log(`Installing ${PKG} globally...`);
  }
  run("npm", ["install", "-g", PKG], {
    timeout: 120000,
    env: { ...process.env, PIPPIT_CLI_SKIP_SKILLS: "1" },
  });

  console.log("Installing pippit-tool-cli skills...");
  try {
    installGlobalPackageSkills(DEFAULT_PKG);
  } catch (err) {
    if (!installed) {
      throw err;
    }
    console.log("Existing global package does not contain skills; reinstalling...");
    run("npm", ["install", "-g", PKG], { timeout: 120000 });
    installGlobalPackageSkills(DEFAULT_PKG);
  }

  const bin = whichPippitToolCli();
  if (!bin) {
    console.error("pippit-tool-cli was installed, but no global command was found in npm prefix.");
    console.error("Check that npm's global bin directory is in PATH.");
    process.exit(1);
  }

  console.log(`pippit-tool-cli is ready: ${bin}`);
  reportBundledSkillTelemetry("install", "npx_install");
  console.log("Try: pippit-tool-cli short-drama +submit-run --message \"写一个短剧开头\"");
}

main();

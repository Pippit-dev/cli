#!/usr/bin/env node

const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const { maybeWarnNewVersion } = require("./version-check");

const ext = process.platform === "win32" ? ".exe" : "";
const bin = path.join(__dirname, "..", "bin", "pippit-tool-cli" + ext);
const args = process.argv.slice(2);

const oldBin = bin + ".old";
function restoreOldBinary() {
  try {
    if (fs.existsSync(bin)) {
      fs.rmSync(bin, { force: true });
    }
    fs.renameSync(oldBin, bin);
    return true;
  } catch (_) {
    return false;
  }
}

if (process.platform === "win32" && fs.existsSync(oldBin)) {
  if (!fs.existsSync(bin)) {
    restoreOldBinary();
  } else {
    try {
      execFileSync(bin, ["--help"], { stdio: "ignore", timeout: 10000 });
      fs.rmSync(oldBin, { force: true });
    } catch (_) {
      restoreOldBinary();
    }
  }
}

// Match the lark-cli install entry: `npx @pippit-dev/cli@latest install`
// should run the JS setup flow before the native binary exists.
if (args[0] === "install") {
  require("./install-wizard.js").main();
} else {
  maybeWarnNewVersion(args);

  if (!fs.existsSync(bin)) {
    try {
      execFileSync(process.execPath, [path.join(__dirname, "install.js")], {
        stdio: "inherit",
        env: { ...process.env, PIPPIT_CLI_RUN: "true" },
      });
    } catch (_) {
      console.error(
        "\nFailed to prepare pippit-tool-cli binary.\n" +
        "Make sure Go is installed and available in PATH, then retry.\n"
      );
      process.exit(1);
    }
  }

  try {
    execFileSync(bin, args, { stdio: "inherit" });
  } catch (e) {
    process.exit(e.status || 1);
  }
}

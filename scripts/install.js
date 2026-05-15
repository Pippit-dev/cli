#!/usr/bin/env node

const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const NAME = "pippit-cli";
const ROOT = path.join(__dirname, "..");
const BIN_DIR = path.join(ROOT, "bin");
const EXT = process.platform === "win32" ? ".exe" : "";
const DEST = path.join(BIN_DIR, NAME + EXT);

function run(cmd, args, opts = {}) {
  execFileSync(cmd, args, { stdio: "inherit", ...opts });
}

function ensureGo() {
  try {
    execFileSync("go", ["version"], { stdio: "ignore" });
  } catch (_) {
    throw new Error("Go is required to build pippit-cli from the npm package");
  }
}

function install() {
  ensureGo();
  fs.mkdirSync(BIN_DIR, { recursive: true });
  run("go", ["build", "-o", DEST, "."], { cwd: ROOT });
  fs.chmodSync(DEST, 0o755);
  console.log(`${NAME} installed successfully`);
}

if (require.main === module) {
  // Under `npx @pippit-dev/cli@latest install`, the temporary package only
  // needs run.js + install-wizard.js. The wizard performs the real global
  // install, whose postinstall then builds the persistent binary.
  const isNpxPostinstall =
    process.env.npm_command === "exec" && !process.env.PIPPIT_CLI_RUN;

  if (isNpxPostinstall) {
    process.exit(0);
  }

  try {
    install();
  } catch (err) {
    console.error(`Failed to install ${NAME}: ${err.message || err}`);
    console.error(
      "\nTry:\n" +
      "  go version\n" +
      "  npm install -g @pippit-dev/cli\n"
    );
    process.exit(1);
  }
}

module.exports = { install };

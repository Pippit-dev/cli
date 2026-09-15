#!/usr/bin/env node

const { install } = require("./install");

if (require.main === module) {
  if (process.argv.length === 3 && process.argv[2] === "--help") {
    console.log("Usage: node scripts/install-cli.js\nInstall this package's CLI binary without changing global Skills.");
  } else if (process.argv.length !== 2) {
    console.error("Unsupported arguments. Usage: node scripts/install-cli.js");
    process.exitCode = 1;
  } else {
    try {
      install({ cliOnly: true });
    } catch (err) {
      console.error(`Failed to install pippit-tool-cli: ${err.message || err}`);
      process.exitCode = 1;
    }
  }
}

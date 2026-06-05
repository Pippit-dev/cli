const assert = require("assert");

const { defaultInstallPackage, installPackage } = require("./install-wizard");
const { DEFAULT_PKG } = require("./skills");

const version = require("../package.json").version.replace(/-.*$/, "");

delete process.env.PIPPIT_CLI_INSTALL_PACKAGE;
assert.strictEqual(defaultInstallPackage(), `${DEFAULT_PKG}@${version}`);
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@${version}`);

process.env.PIPPIT_CLI_INSTALL_PACKAGE = `${DEFAULT_PKG}@0.0.26`;
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@0.0.26`);

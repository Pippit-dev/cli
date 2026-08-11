const assert = require("assert");

const {
  defaultInstallPackage,
  exactPackageVersion: wizardPackageVersion,
  installPackage,
} = require("./install-wizard");
const {
  archiveName,
  exactPackageVersion: installerPackageVersion,
  releaseURL,
} = require("./install");
const { DEFAULT_PKG } = require("./skills");

const version = require("../package.json").version;

delete process.env.PIPPIT_CLI_INSTALL_PACKAGE;
assert.strictEqual(defaultInstallPackage(), `${DEFAULT_PKG}@${version}`);
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@${version}`);

process.env.PIPPIT_CLI_INSTALL_PACKAGE = `${DEFAULT_PKG}@0.0.26`;
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@0.0.26`);

assert.strictEqual(wizardPackageVersion("1.1.0-beta.3"), "1.1.0-beta.3");
assert.strictEqual(installerPackageVersion("1.1.0-beta.3"), "1.1.0-beta.3");
assert.ok(archiveName.includes(`-${version}-`), archiveName);
assert.ok(releaseURL.includes(`/download/v${version}/`), releaseURL);

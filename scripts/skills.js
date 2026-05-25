const fs = require("fs");
const path = require("path");
const { run, runSilent } = require("./platform");

const DEFAULT_PKG = "@pippit-dev/cli";

function installSkillsFromRoot(root, opts = {}) {
  const source = path.resolve(root);
  const skillsDir = path.join(source, "skills");
  if (!fs.existsSync(skillsDir)) {
    throw new Error(`skills directory not found: ${skillsDir}`);
  }
  run("npx", ["-y", "skills", "add", source, "-g", "-y"], {
    timeout: opts.timeout || 120000,
  });
}

function globalPackageRoot(pkg = DEFAULT_PKG) {
  const npmRoot = runSilent("npm", ["root", "-g"], { timeout: 15000 }).toString().trim();
  return path.join(npmRoot, ...pkg.split("/"));
}

function installGlobalPackageSkills(pkg = DEFAULT_PKG, opts = {}) {
  installSkillsFromRoot(globalPackageRoot(pkg), opts);
}

module.exports = {
  DEFAULT_PKG,
  globalPackageRoot,
  installGlobalPackageSkills,
  installSkillsFromRoot,
};

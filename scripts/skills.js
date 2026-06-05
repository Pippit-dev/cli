const fs = require("fs");
const os = require("os");
const path = require("path");
const { run, runSilent } = require("./platform");

const DEFAULT_PKG = "@pippit-dev/cli";
const LEGACY_GLOBAL_SKILLS = [
  "pippit-short-drama-skill",
  "xyq-nest-skill",
];

function defaultGlobalSkillsDir() {
  return path.join(os.homedir(), ".agents", "skills");
}

function cleanupLegacyGlobalSkills(globalSkillsDir = defaultGlobalSkillsDir()) {
  for (const skillName of LEGACY_GLOBAL_SKILLS) {
    fs.rmSync(path.join(globalSkillsDir, skillName), {
      force: true,
      recursive: true,
    });
  }
}

function installSkillsFromRoot(root, opts = {}) {
  const source = path.resolve(root);
  const skillsDir = path.join(source, "skills");
  if (!fs.existsSync(skillsDir)) {
    throw new Error(`skills directory not found: ${skillsDir}`);
  }
  run("npx", ["-y", "skills", "add", source, "-g", "-y", "--skill", "*"], {
    timeout: opts.timeout || 120000,
  });
  cleanupLegacyGlobalSkills(opts.globalSkillsDir);
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
  LEGACY_GLOBAL_SKILLS,
  cleanupLegacyGlobalSkills,
  defaultGlobalSkillsDir,
  globalPackageRoot,
  installGlobalPackageSkills,
  installSkillsFromRoot,
};

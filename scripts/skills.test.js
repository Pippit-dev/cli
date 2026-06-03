const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { cleanupLegacyGlobalSkills } = require("./skills");

const globalSkillsDir = fs.mkdtempSync(path.join(os.tmpdir(), "pippit-skills-test-"));

for (const skillName of [
  "pippit-short-drama-skill",
  "xyq-nest-skill",
  "xyq-short-drama-skill",
  "xyq-skill",
]) {
  fs.mkdirSync(path.join(globalSkillsDir, skillName));
}

cleanupLegacyGlobalSkills(globalSkillsDir);

assert.strictEqual(fs.existsSync(path.join(globalSkillsDir, "pippit-short-drama-skill")), false);
assert.strictEqual(fs.existsSync(path.join(globalSkillsDir, "xyq-nest-skill")), false);
assert.strictEqual(fs.existsSync(path.join(globalSkillsDir, "xyq-short-drama-skill")), true);
assert.strictEqual(fs.existsSync(path.join(globalSkillsDir, "xyq-skill")), true);

fs.rmSync(globalSkillsDir, { force: true, recursive: true });

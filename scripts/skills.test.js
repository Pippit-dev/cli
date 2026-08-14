const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { cleanupLegacyGlobalSkills } = require("./skills");

const repoRoot = path.resolve(__dirname, "..");
const generalSkillPath = path.join(repoRoot, "skills", "xyq-nest-skill", "SKILL.md");
const shortDramaSkillPath = path.join(repoRoot, "skills", "short-drama", "SKILL.md");
const readmePath = path.join(repoRoot, "README.md");

function readRequiredFile(filePath) {
  assert.strictEqual(fs.existsSync(filePath), true, `missing required file: ${filePath}`);
  return fs.readFileSync(filePath, "utf8");
}

function assertFrontmatterName(content, expectedName) {
  const match = content.match(/^---\n[\s\S]*?^name:\s*([^\n]+)$/m);
  assert.ok(match, `missing frontmatter name for ${expectedName}`);
  assert.strictEqual(match[1].trim(), expectedName);
}

const generalSkill = readRequiredFile(generalSkillPath);
const shortDramaSkill = readRequiredFile(shortDramaSkillPath);
const readme = readRequiredFile(readmePath);

assertFrontmatterName(generalSkill, "xyq-skill");
assertFrontmatterName(shortDramaSkill, "xyq-short-drama-skill");
assert.ok(generalSkill.includes("user-invocable: true"), "xyq-skill must remain user-invocable");
assert.ok(
  shortDramaSkill.includes("user-invocable: true"),
  "xyq-short-drama-skill must remain user-invocable",
);

for (const requiredText of [
  "pippit-tool-cli generate-video",
  "pippit-tool-cli query-result",
  "pippit-tool-cli login",
  "XYQ_ACCESS_KEY",
  "submit_run.py",
  "web_thread_link",
  "request_user_input",
  "ask_user_question",
  "xyq-short-drama-skill",
]) {
  assert.ok(generalSkill.includes(requiredText), `xyq-skill missing contract: ${requiredText}`);
}

for (const requiredText of ["request_user_input", "ask_user_question", "credits"]) {
  assert.ok(
    shortDramaSkill.includes(requiredText),
    `xyq-short-drama-skill missing contract: ${requiredText}`,
  );
}

for (const requiredText of [
  "skills/xyq-nest-skill/",
  "skills/short-drama/",
  "pippit-tool-cli generate-video",
  "request_user_input",
  "ask_user_question",
]) {
  assert.ok(readme.includes(requiredText), `README missing skill contract: ${requiredText}`);
}

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

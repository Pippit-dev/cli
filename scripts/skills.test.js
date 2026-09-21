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

for (const content of [generalSkill, shortDramaSkill]) {
  for (const expected of ["--source", "doubao_office", "workbuddy", "codex", "静默", "统计"]) {
    assert(content.includes(expected), `Skill missing source attribution contract: ${expected}`);
  }
}

// The Skill is a self-contained document graph: follow only the selected module
// at runtime, but verify all shipped references and examples offline here.
const skillRoot = path.dirname(generalSkillPath);
const visited = new Set();
function visitDocument(filePath) {
  filePath = path.resolve(filePath);
  assert(filePath.startsWith(skillRoot + path.sep), `Skill reference escapes its package: ${filePath}`);
  if (visited.has(filePath)) return;
  visited.add(filePath);
  const content = readRequiredFile(filePath);
  for (const match of content.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
    const target = match[1].split("#")[0];
    if (!target || /^[a-z]+:/i.test(target)) continue;
    const resolved = path.resolve(path.dirname(filePath), target);
    assert(fs.existsSync(resolved), `Broken Skill link in ${filePath}: ${target}`);
    if (resolved.endsWith(".md")) visitDocument(resolved);
  }
}
visitDocument(generalSkillPath);
const skillDocuments = [...visited].map((file) => readRequiredFile(file)).join("\n");
const commandModules = {
  auth: ["status", "login", "logout"],
  canvas: ["canvas"],
  "generate-image": ["generate-image"],
  "generate-video": ["generate-video"],
  model: ["model"],
  "video-super-resolution": ["video-super-resolution"],
  "erase-video-subtitle": ["erase-video-subtitle"],
  "query-result": ["query-result"],
  "get-credit-balance": ["get-credit-balance"],
};
for (const [moduleName, commands] of Object.entries(commandModules)) {
  const file = path.join(skillRoot, "commands", `${moduleName}.md`);
  assert(visited.has(file), `Module is not reachable from SKILL.md: ${moduleName}`);
  for (const command of commands) {
    assert(readRequiredFile(file).includes(`pippit-tool-cli ${command}`), `Module missing usage: ${command}`);
  }
}
const documentedCommands = [...new Set([...skillDocuments.matchAll(/\bpippit-tool-cli ([a-z][a-z-]*)\b/g)].map((match) => match[1]))].sort();
assert.deepStrictEqual(documentedCommands, Object.values(commandModules).flat().sort(), "Skill must document exactly its supported CLI commands");
for (const requiredText of ["XYQ_ACCESS_KEY", "web_thread_link", "request_user_input", "ask_user_question"]) {
  assert(skillDocuments.includes(requiredText), `xyq-skill missing contract: ${requiredText}`);
}
for (const folder of ["commands", "workflows", "examples"]) {
  for (const file of fs.readdirSync(path.join(skillRoot, folder))) {
    if (file.endsWith(".md")) {
      assert(visited.has(path.join(skillRoot, folder, file)), `Unreachable Skill document: ${folder}/${file}`);
    }
  }
}
assert(visited.has(path.join(skillRoot, "examples", "generate-and-deliver.md")), "Keep the basic end-to-end example");

function checkSkillFiles(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) checkSkillFiles(file);
    else if (/\.(md|js)$/.test(entry.name)) {
      const content = readRequiredFile(file);
      assert(!/submit[-_]run|get[-_]thread|upload[-_]file|download[-_]results?/.test(content), `Removed command still in Skill: ${file}`);
      assert(!content.includes("xyq-short-drama-skill"), `Unexpected Skill dependency: ${file}`);
    }
  }
}
checkSkillFiles(skillRoot);

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

for (const script of ["submit_run.py", "upload_file.py", "download_results.py", "get_thread.py", "xyq_common.py"]) {
  assert.ok(!generalSkill.includes(script), `xyq-skill must migrate ${script} to CLI`);
  assert.ok(!readme.includes(script), `README must migrate ${script} to CLI`);
  assert.strictEqual(
    fs.existsSync(path.join(repoRoot, "skills", "xyq-nest-skill", "scripts", script)),
    false,
    `legacy ${script} must be removed`,
  );
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

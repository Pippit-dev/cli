const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

// Execute the actual workflow body with isolated npm/gh stubs; never contact a registry.
const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/rollback.yml"), "utf8");
const match = workflow.match(/^        run: \|\n((?:          .*\n|\n)+)$/m);
assert(match, "Expected one inline rollback step");
const script = match[1].replace(/^          /gm, "");
assert(!script.includes("${{"), "Dispatch inputs must reach the shell through env");
const root = fs.mkdtempSync(path.join(os.tmpdir(), "cli-rollback-test-"));
const bin = path.join(root, "bin");
fs.mkdirSync(bin);
fs.symlinkSync(process.execPath, path.join(bin, "node"));
const stub = `#!${process.execPath}
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const file = path.join(process.env.RUNNER_TEMP, 'state.json');
const state = JSON.parse(fs.readFileSync(file, 'utf8'));
const tool = path.basename(process.argv[1]);
const args = process.argv.slice(2);
state.calls.push([tool, ...args]);
function finish(value, status = 0) {
  fs.writeFileSync(file, JSON.stringify(state));
  if (value !== undefined) console.log(value);
  process.exit(status);
}
if (tool === 'gh') {
  assert.deepStrictEqual(args, ['api', 'repos/Pippit-dev/cli/releases/tags/v' + process.env.TARGET_VERSION]);
  finish(JSON.stringify(state.release), state.fail === 'release' ? 1 : 0);
}
if (args[0] === 'view') {
  assert.strictEqual(args[3], '--prefer-online');
  if (args[1] === '@pippit-dev/cli') {
    // Package metadata can remain unchanged after the tag endpoint accepts a write.
    finish(state.metadataTags[args[2].replace(/^dist-tags\\./, '')]);
  }
  assert.deepStrictEqual(args, ['view', '@pippit-dev/cli@' + process.env.TARGET_VERSION, 'version', '--prefer-online']);
  finish(state.npmVersion, state.fail === 'target' ? 1 : 0);
}
if (args[0] === 'dist-tag' && args[1] === 'ls') {
  assert.deepStrictEqual(args, ['dist-tag', 'ls', '@pippit-dev/cli', '--prefer-online']);
  const tag = process.env.CHANNEL === 'stable' ? 'latest' : 'canary';
  state.reads++;
  if (state.reads === 2 && state.changedDuringCheck) state.tags[tag] = '1.0.40';
  const tags = { ...state.tags };
  if (state.reads === 3 && state.badReadback) tags[tag] = '1.0.40';
  const value = Object.keys(tags).sort().map(tag => tag + ': ' + tags[tag]).join('\\n');
  finish(value, state.fail === 'current' || (state.reads === 3 && state.fail === 'readback') ? 1 : 0);
}
assert.deepStrictEqual(args.slice(0, 3), ['dist-tag', 'add', '@pippit-dev/cli@' + process.env.TARGET_VERSION]);
assert(['latest', 'canary'].includes(args[3]));
if (state.fail === 'write') finish(undefined, 1);
state.tags[args[3]] = process.env.TARGET_VERSION;
finish();
`;
for (const tool of ["npm", "gh"]) fs.writeFileSync(path.join(bin, tool), stub, { mode: 0o755 });

const assetNames = ["checksums.txt", ...["darwin", "linux", "windows"].flatMap(platform =>
  ["amd64", "arm64"].map(arch => `pippit-tool-cli-1.0.38-${platform}-${arch}.${platform === "windows" ? "zip" : "tar.gz"}`)
)];
let passed = 0;
function check(name, { input = {}, change = () => {}, success = false, writes = 0 } = {}) {
  const dir = fs.mkdtempSync(path.join(root, "case-"));
  const state = {
    tags: { latest: "1.0.39", canary: "1.0.39" }, reads: 0, calls: [], npmVersion: "1.0.38",
    release: { tag_name: "v1.0.38", draft: false, assets: assetNames.map(name => ({ name, state: "uploaded", size: 123 })) },
  };
  change(state);
  state.metadataTags = { ...state.tags };
  fs.writeFileSync(path.join(dir, "state.json"), JSON.stringify(state));
  const env = {
    PATH: `${bin}:/usr/bin:/bin`, HOME: dir, RUNNER_TEMP: dir,
    GITHUB_STEP_SUMMARY: path.join(dir, "summary"),
    CHANNEL: "stable", EXPECTED_VERSION: "1.0.39", TARGET_VERSION: "1.0.38", ...input,
  };
  const result = spawnSync("/bin/bash", ["--noprofile", "--norc", "-c", script], {
    cwd: dir, env, encoding: "utf8", timeout: 10000,
  });
  assert.ifError(result.error);
  assert.strictEqual(result.status === 0, success, `${name}: ${result.stdout}\n${result.stderr}`);
  const after = JSON.parse(fs.readFileSync(path.join(dir, "state.json"), "utf8"));
  const mutations = after.calls.filter(call => call[1] === "dist-tag" && call[2] === "add");
  assert.strictEqual(mutations.length, writes, `${name}: unexpected npm writes`);
  const tag = env.CHANNEL === "stable" ? "latest" : "canary";
  const otherTag = tag === "latest" ? "canary" : "latest";
  assert.strictEqual(after.tags[otherTag], state.tags[otherTag], `${name}: changed another channel`);
  if (writes) assert.deepStrictEqual(mutations[0], ["npm", "dist-tag", "add", `@pippit-dev/cli@${env.TARGET_VERSION}`, tag]);
  if (success) {
    assert.strictEqual(after.tags[tag], env.TARGET_VERSION);
    const summary = fs.readFileSync(env.GITHUB_STEP_SUMMARY, "utf8");
    assert(summary.includes(`${env.EXPECTED_VERSION} -> ${env.TARGET_VERSION}`));
    assert(summary.includes(`Confirmed: @pippit-dev/cli ${tag} now points to ${env.TARGET_VERSION}`));
  } else if (fs.existsSync(env.GITHUB_STEP_SUMMARY)) {
    assert(!fs.readFileSync(env.GITHUB_STEP_SUMMARY, "utf8").includes("Confirmed:"));
  }
  passed++;
}

try {
  check("stable with stale package metadata after write", { success: true, writes: 1 });
  check("canary with stale package metadata after write", { input: { CHANNEL: "canary" }, success: true, writes: 1 });
  check("numeric ordering", { input: { EXPECTED_VERSION: "1.0.100" }, change: s => { s.tags.latest = "1.0.100"; }, success: true, writes: 1 });
  check("invalid channel", { input: { CHANNEL: "latest" } });
  for (const version of ["", "v1.0.38", "01.0.38", "1.0.38-rc.1", "1.0.38\nnpm publish", "$(touch injected)"]) {
    check(`invalid target ${version}`, { input: { TARGET_VERSION: version } });
  }
  check("invalid expected version", { input: { EXPECTED_VERSION: "1.0.39; npm publish" } });
  check("same version", { input: { TARGET_VERSION: "1.0.39" } });
  check("upgrade", { input: { TARGET_VERSION: "1.0.40" } });
  check("numeric upgrade", { input: { EXPECTED_VERSION: "1.0.9" } });
  check("stale current", { change: s => { s.tags.latest = "1.0.40"; } });
  check("missing channel", { change: s => { delete s.tags.latest; } });
  check("changed during artifact check", { change: s => { s.changedDuringCheck = true; } });
  check("wrong npm version", { change: s => { s.npmVersion = "1.0.37"; } });
  check("draft release", { change: s => { s.release.draft = true; } });
  check("wrong release tag", { change: s => { s.release.tag_name = "v1.0.37"; } });
  for (const name of assetNames) {
    check(`missing ${name}`, { change: s => { s.release.assets = s.release.assets.filter(asset => asset.name !== name); } });
  }
  check("empty asset", { change: s => { s.release.assets[1].size = 0; } });
  check("unfinished upload", { change: s => { s.release.assets[1].state = "new"; } });
  for (const fail of ["current", "target", "release", "write", "readback"]) {
    check(`${fail} failure`, { change: s => { s.fail = fail; }, writes: ["write", "readback"].includes(fail) ? 1 : 0 });
  }
  check("readback mismatch", { change: s => { s.badReadback = true; }, writes: 1 });
  console.log(`Rollback workflow: ${passed} checks passed (no network or npm writes).`);
} finally {
  fs.rmSync(root, { recursive: true, force: true });
}

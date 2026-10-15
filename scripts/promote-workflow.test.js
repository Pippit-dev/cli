const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

// Execute the workflow against an npm stub whose package metadata remains stale after a tag write.
const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/promote.yml"), "utf8");
const match = workflow.match(/^        run: \|\n((?:          .*\n|\n)+)$/m);
assert(match, "Expected one inline promotion step");
const script = match[1].replace(/^          /gm, "");
assert(!script.includes("${{"), "Dispatch inputs must reach the shell through env");
const root = fs.mkdtempSync(path.join(os.tmpdir(), "cli-promote-test-"));
const bin = path.join(root, "bin");
fs.mkdirSync(bin);
fs.writeFileSync(path.join(bin, "npm"), `#!${process.execPath}
const assert = require('assert');
const fs = require('fs');
const file = process.env.STATE_FILE;
const state = JSON.parse(fs.readFileSync(file, 'utf8'));
const args = process.argv.slice(2);
state.calls.push(args);
function finish(value, status = 0) {
  fs.writeFileSync(file, JSON.stringify(state));
  if (value !== undefined) console.log(value);
  process.exit(status);
}
if (args[0] === 'view') {
  assert.strictEqual(args[1], '@pippit-dev/cli');
  assert.strictEqual(args[3], '--prefer-online');
  finish(state.metadataTags[args[2].replace(/^dist-tags\\./, '')]);
}
if (args[0] === 'dist-tag' && args[1] === 'ls') {
  assert.deepStrictEqual(args, ['dist-tag', 'ls', '@pippit-dev/cli', '--prefer-online']);
  state.reads++;
  const tags = { ...state.tags };
  if (state.reads === 2 && state.badReadback) tags.latest = '1.0.38';
  if (state.reads === 2 && state.missingReadback) delete tags.latest;
  const value = Object.keys(tags).sort().map(tag => tag + ': ' + tags[tag]).join('\\n');
  finish(value, state.fail === 'current' || (state.reads === 2 && state.fail === 'readback') ? 1 : 0);
}
assert.deepStrictEqual(args, ['dist-tag', 'add', '@pippit-dev/cli@' + process.env.RELEASE_VERSION, 'latest']);
if (state.fail === 'write') finish(undefined, 1);
state.tags.latest = process.env.RELEASE_VERSION;
finish('+latest: @pippit-dev/cli@' + process.env.RELEASE_VERSION);
`, { mode: 0o755 });

let passed = 0;
function check(name, { version = "1.0.41", change = () => {}, success = false, writes = 0 } = {}) {
  const dir = fs.mkdtempSync(path.join(root, "case-"));
  const file = path.join(dir, "state.json");
  const state = { tags: { latest: "1.0.38", canary: "1.0.41" }, reads: 0, calls: [] };
  change(state);
  state.metadataTags = { ...state.tags };
  fs.writeFileSync(file, JSON.stringify(state));
  const result = spawnSync("/bin/bash", ["--noprofile", "--norc", "-c", script], {
    cwd: dir, encoding: "utf8", timeout: 10000,
    env: { PATH: `${bin}:/usr/bin:/bin`, HOME: dir, STATE_FILE: file, RELEASE_VERSION: version },
  });
  assert.ifError(result.error);
  assert.strictEqual(result.status === 0, success, `${name}: ${result.stdout}\n${result.stderr}`);
  const after = JSON.parse(fs.readFileSync(file, "utf8"));
  assert.strictEqual(after.calls.filter(args => args[0] === "dist-tag" && args[1] === "add").length, writes, name);
  assert.strictEqual(after.tags.canary, state.tags.canary, `${name}: changed canary`);
  if (success) {
    assert.strictEqual(after.tags.latest, version);
    assert(result.stdout.includes(`Promoted @pippit-dev/cli@${version} to latest.`));
  } else {
    assert(!result.stdout.includes("Promoted "), `${name}: reported success after a failure`);
  }
  passed++;
}

try {
  check("stale package metadata after successful promotion", { success: true, writes: 1 });
  check("already promoted", { change: s => { s.tags.latest = "1.0.41"; }, success: true, writes: 1 });
  for (const version of ["", "v1.0.41", "1.0.41-rc.1", "$(touch injected)"]) {
    check(`invalid version ${version}`, { version });
  }
  check("candidate changed", { change: s => { s.tags.canary = "1.0.42"; } });
  check("missing canary", { change: s => { delete s.tags.canary; } });
  for (const fail of ["current", "write", "readback"]) {
    check(`${fail} failure`, { change: s => { s.fail = fail; }, writes: fail === "current" ? 0 : 1 });
  }
  check("readback mismatch", { change: s => { s.badReadback = true; }, writes: 1 });
  check("missing readback tag", { change: s => { s.missingReadback = true; }, writes: 1 });
  console.log(`Promote workflow: ${passed} checks passed (no network or npm writes).`);
} finally {
  fs.rmSync(root, { recursive: true, force: true });
}

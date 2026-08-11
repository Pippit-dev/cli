const assert = require("assert");
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const root = path.join(__dirname, "..");
const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "pippit-libtv-run-"));
const output = path.join(outputDir, "plan.json");

assert.deepStrictEqual(require("../package.json").bin, {
  "pippit-tool-cli": "scripts/run.js",
});

try {
  const result = spawnSync(process.execPath, [
    path.join(__dirname, "run.js"),
    "libtv",
    "plan",
    "--snapshot",
    path.join(root, "adapters", "libtv", "testdata", "snapshot.json"),
    "--output",
    output,
  ], {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, PIPPIT_CLI_SKIP_VERSION_CHECK: "1" },
  });

  assert.strictEqual(result.status, 0, result.stderr);
  assert.strictEqual(JSON.parse(fs.readFileSync(output, "utf8")).schema, "pippit-canvas-plan/0.1");
  assert.strictEqual(JSON.parse(result.stdout).schema, "pippit-canvas-plan/0.1");
} finally {
  fs.rmSync(outputDir, { recursive: true, force: true });
}

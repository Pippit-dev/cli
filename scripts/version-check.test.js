const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const vm = require("vm");
const { CHECK_INTERVAL_MS, defaultCacheFile, maybeWarnNewVersion } = require("./version-check");
const { isCanvasCommand, parseCanvasCommandArgs } = require("./canvas-command");

assert.strictEqual(
  defaultCacheFile(),
  path.join(os.homedir(), ".pippit_tool_cli", "version-check.json")
);

const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "pippit-cli-version-check-"));
try {
  const cacheFile = path.join(fixture, "version-check.json");
  const warnings = [];
  let fetches = 0;
  const opts = {
    env: {}, cacheFile, currentVersion: "1.0.41", now: 1000,
    fetchLatestVersion: () => { fetches += 1; return "1.0.42"; },
    warn: (message) => warnings.push(message),
  };

  maybeWarnNewVersion(["login"], opts);
  assert.strictEqual(fetches, 1);
  assert.strictEqual(warnings.length, 2);
  assert(warnings[0].includes("1.0.41 -> 1.0.42"));
  assert(warnings[0].includes("pippit-tool-cli update"));
  assert(warnings[1].includes("pippit-tool-cli login again"));

  warnings.length = 0;
  maybeWarnNewVersion(["canvas", "command", "list"], { ...opts, now: 2000 });
  assert.strictEqual(fetches, 1, "fresh cache should avoid a second network call");
  assert.strictEqual(warnings.length, 1, "discovery keeps the normal update notice");

  maybeWarnNewVersion(["status"], { ...opts, now: 1000 + CHECK_INTERVAL_MS });
  assert.strictEqual(fetches, 2, "expired cache should check again");
  warnings.length = 0;
  maybeWarnNewVersion(["login"], { ...opts, currentVersion: "1.0.42" });
  assert.deepStrictEqual(warnings, [], "login should not imply an update exists when current");

  for (const [args, env] of [
    [["login"], { CI: "1" }],
    [["login"], { PIPPIT_CLI_DISABLE_UPDATE_CHECK: "1" }],
    [["install"], {}], [["update"], {}],
  ]) {
    maybeWarnNewVersion(args, { ...opts, env, now: 10 * CHECK_INTERVAL_MS });
  }
  assert.strictEqual(fetches, 2, "CI, opt-out and update/install must stay silent");
  assert.deepStrictEqual(warnings, []);

  const failedCache = path.join(fixture, "failed-check.json");
  maybeWarnNewVersion(["login"], {
    ...opts, cacheFile: failedCache,
    fetchLatestVersion: () => { throw new Error("offline"); },
  });
  assert.deepStrictEqual(warnings, [], "network failure must stay silent");
  assert.strictEqual(fs.existsSync(failedCache), false);

  const originalError = console.error;
  try {
    console.error = (message) => warnings.push(message);
    maybeWarnNewVersion(["status"], { ...opts, warn: undefined });
    assert.strictEqual(warnings.length, 1, "default notices go through stderr console.error");
  } finally {
    console.error = originalError;
  }

  // Run the actual launcher with controlled dependencies: no native process,
  // installer, SDK load or network call is allowed in this routing check.
  function launch(args) {
    const notices = [];
    const invocations = [];
    vm.runInNewContext(fs.readFileSync(path.join(__dirname, "run.js"), "utf8"), {
      __dirname,
      process: { argv: ["node", "run.js", ...args], platform: "linux", env: {} },
      console: { error: () => {} },
      require(name) {
        if (name === "fs") return { existsSync: () => true };
        if (name === "path") return path;
        if (name === "child_process") return { execFileSync: () => invocations.push("native") };
        if (name === "./version-check") return { maybeWarnNewVersion: (actualArgs) => notices.push([...actualArgs]) };
        if (name === "./install-wizard.js") return { main: () => invocations.push("install") };
        if (name === "./canvas-command") return {
          isCanvasCommand, parseCanvasCommandArgs,
          runCanvasCommand: () => { invocations.push("canvas"); return Promise.resolve(0); },
        };
        throw new Error(`Unexpected launcher dependency: ${name}`);
      },
    });
    return { notices, invocations };
  }

  for (const args of [
    ["login"], ["status"], ["canvas", "command", "list"],
    ["canvas", "command", "describe", "example"],
    ["canvas", "command", "schema"], ["canvas", "command", "guide"],
    ["canvas", "command", "run", "example", "--canvas-id", "test", "--input", "{}"],
  ]) {
    assert.deepStrictEqual(launch(args).notices, [args], `missing or duplicate check: ${args.join(" ")}`);
  }
  for (const args of [
    [], ["--help"], ["help", "canvas"], ["install", "--help"],
    ["canvas", "command"], ["canvas", "command", "--help"],
    ["canvas", "command", "list", "--help"],
    ["canvas", "command", "unknown-action"],
    ["canvas", "command", "describe"],
  ]) {
    assert.deepStrictEqual(launch(args).notices, [], `unexpected update check: ${args.join(" ")}`);
  }
  console.log("Version checks: cache, silent failures, stderr and native/canvas launcher routing passed.");
} finally {
  fs.rmSync(fixture, { recursive: true, force: true });
}

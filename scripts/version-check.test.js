const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const vm = require("vm");
const { CHECK_INTERVAL_MS, RETRY_INTERVAL_MS, defaultCacheFile, maybeWarnNewVersion } = require("./version-check");
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
  let failedFetches = 0;
  const offlineOptions = {
    ...opts, cacheFile: failedCache,
    fetchLatestVersion: () => { failedFetches += 1; throw new Error("offline"); },
  };
  maybeWarnNewVersion(["login"], offlineOptions);
  maybeWarnNewVersion(["status"], { ...offlineOptions, now: 2000 });
  assert.deepStrictEqual(warnings, [], "network failure must stay silent");
  assert.strictEqual(failedFetches, 1, "offline invocations share a short retry cooldown");
  maybeWarnNewVersion(["status"], { ...offlineOptions, now: 1000 + RETRY_INTERVAL_MS });
  assert.strictEqual(failedFetches, 2, "a failed check retries after the short cooldown");

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
    const nativeEnvironments = [];
    vm.runInNewContext(fs.readFileSync(path.join(__dirname, "run.js"), "utf8"), {
      __dirname,
      process: { argv: ["node", "run.js", ...args], platform: "linux", env: {} },
      console: { error: () => {} },
      require(name) {
        if (name === "fs") return { existsSync: () => true };
        if (name === "path") return path;
        if (name === "child_process") return {
          execFileSync: (_command, _args, options) => {
            invocations.push("native");
            nativeEnvironments.push(options.env);
          },
        };
        if (name === "./version-check") return { maybeWarnNewVersion: (actualArgs) => notices.push([...actualArgs]) };
        if (name === "./install-wizard.js") return { main: () => invocations.push("install") };
        if (name === "./canvas-command") return {
          isCanvasCommand, parseCanvasCommandArgs,
          runCanvasCommand: (_args, options) => {
            invocations.push("canvas");
            nativeEnvironments.push(options.nativeInvocation.env);
            return Promise.resolve(0);
          },
        };
        throw new Error(`Unexpected launcher dependency: ${name}`);
      },
    });
    return { notices, invocations, nativeEnvironments };
  }

  for (const args of [
    ["login"], ["status"], ["canvas", "command", "list"],
    ["canvas", "command", "describe", "example"],
    ["canvas", "command", "schema"], ["canvas", "command", "guide"],
    ["canvas", "command", "run", "example", "--canvas-id", "test", "--input", "{}"],
  ]) {
    const result = launch(args);
    assert.deepStrictEqual(result.notices, [args], `missing or duplicate check: ${args.join(" ")}`);
    assert.strictEqual(result.nativeEnvironments.length, 1);
    assert.strictEqual(result.nativeEnvironments[0].PIPPIT_CLI_UPDATE_CHECKED, "1", "native children must not repeat the launcher check");
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

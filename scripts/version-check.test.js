const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const {
  compareSemver,
  defaultCacheFile,
  distTagForVersion,
  maybeWarnNewVersion,
  parseSemver,
} = require("./version-check");

assert.strictEqual(
  defaultCacheFile(),
  path.join(require("os").homedir(), ".pippit_tool_cli", "version-check.json")
);

assert.deepStrictEqual(parseSemver("v1.2.3-beta.4+build.7"), {
  major: 1,
  minor: 2,
  patch: 3,
  prerelease: ["beta", "4"],
});
assert.strictEqual(compareSemver("1.2.3-beta.2", "1.2.3-beta.1"), 1);
assert.strictEqual(compareSemver("1.2.3-beta.10", "1.2.3-beta.2"), 8);
assert.strictEqual(compareSemver("1.2.3", "1.2.3-beta.99"), 1);
assert.strictEqual(compareSemver("1.2.3-beta.1", "1.2.3"), -1);
assert.strictEqual(distTagForVersion("1.2.3-beta.1"), "beta");
assert.strictEqual(distTagForVersion("1.2.3"), "latest");

const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "pippit-version-check-"));
try {
  const cacheFile = path.join(tmpDir, "cache.json");
  const betaWarnings = [];
  let betaFetches = 0;
  maybeWarnNewVersion([], {
    cacheFile,
    currentVersion: "1.1.0-beta.1",
    env: {},
    fetchLatestVersion(pkg, tag) {
      betaFetches += 1;
      assert.strictEqual(pkg, "@pippit-dev/cli");
      assert.strictEqual(tag, "beta");
      return "1.1.0-beta.2";
    },
    now: 1000,
    warn: (message) => betaWarnings.push(message),
  });
  assert.strictEqual(betaFetches, 1);
  assert.strictEqual(betaWarnings.length, 1);
  assert.ok(betaWarnings[0].includes("1.1.0-beta.1 -> 1.1.0-beta.2"));
  assert.ok(betaWarnings[0].includes("npx @pippit-dev/cli@beta install"));
  assert.deepStrictEqual(JSON.parse(fs.readFileSync(cacheFile, "utf8")), {
    channel: "beta",
    latest: "1.1.0-beta.2",
    checkedAt: 1000,
  });

  let stableFetches = 0;
  maybeWarnNewVersion([], {
    cacheFile,
    currentVersion: "1.0.17",
    env: {},
    fetchLatestVersion(_pkg, tag) {
      stableFetches += 1;
      assert.strictEqual(tag, "latest");
      return "1.0.17";
    },
    now: 1001,
  });
  assert.strictEqual(stableFetches, 1, "beta cache must not satisfy latest checks");
} finally {
  fs.rmSync(tmpDir, { recursive: true, force: true });
}

const assert = require("assert");
const path = require("path");
const { CHECK_INTERVAL_MS, defaultCacheFile } = require("./version-check");

assert.strictEqual(CHECK_INTERVAL_MS, 60 * 60 * 1000);

assert.strictEqual(
  defaultCacheFile(),
  path.join(require("os").homedir(), ".pippit_tool_cli", "version-check.json")
);

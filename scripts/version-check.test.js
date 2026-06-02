const assert = require("assert");
const path = require("path");
const { defaultCacheFile } = require("./version-check");

assert.strictEqual(
  defaultCacheFile(),
  path.join(require("os").homedir(), ".pippit_tool_cli", "version-check.json")
);

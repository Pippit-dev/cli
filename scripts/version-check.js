const fs = require("fs");
const os = require("os");
const path = require("path");
const { runSilent } = require("./platform");
const { DEFAULT_PKG } = require("./skills");

const CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;

function defaultCacheFile() {
  return path.join(os.homedir(), ".pippit-cli", "version-check.json");
}

function currentVersion() {
  return require("../package.json").version.replace(/-.*$/, "");
}

function parseSemver(version) {
  const match = String(version || "").trim().match(/^v?(\d+)\.(\d+)\.(\d+)/);
  if (!match) return null;
  return match.slice(1).map(Number);
}

function compareSemver(a, b) {
  const parsedA = parseSemver(a);
  const parsedB = parseSemver(b);
  if (!parsedA || !parsedB) return 0;
  for (let i = 0; i < 3; i++) {
    const diff = parsedA[i] - parsedB[i];
    if (diff !== 0) return diff;
  }
  return 0;
}

function readCache(cacheFile) {
  try {
    return JSON.parse(fs.readFileSync(cacheFile, "utf8"));
  } catch (_) {
    return null;
  }
}

function writeCache(cacheFile, data) {
  try {
    fs.mkdirSync(path.dirname(cacheFile), { recursive: true });
    fs.writeFileSync(cacheFile, JSON.stringify(data), "utf8");
  } catch (_) {
    // Version checks must never block normal CLI commands.
  }
}

function fetchLatestVersion(pkg = DEFAULT_PKG) {
  return runSilent("npm", ["view", pkg, "version"], { timeout: 3000 }).toString().trim();
}

function shouldSkip(args, env) {
  const cmd = args[0];
  return (
    env.PIPPIT_CLI_DISABLE_UPDATE_CHECK === "1" ||
    env.CI ||
    cmd === "install" ||
    cmd === "update"
  );
}

function maybeWarnNewVersion(args = [], opts = {}) {
  const env = opts.env || process.env;
  if (shouldSkip(args, env)) return;

  const now = opts.now || Date.now();
  const cacheFile = opts.cacheFile || defaultCacheFile();
  const cache = readCache(cacheFile);
  const cacheFresh = cache && now - cache.checkedAt < CHECK_INTERVAL_MS;

  let latest = cacheFresh ? cache.latest : "";
  if (!latest) {
    try {
      latest = (opts.fetchLatestVersion || fetchLatestVersion)(opts.pkg || DEFAULT_PKG);
      writeCache(cacheFile, { latest, checkedAt: now });
    } catch (_) {
      return;
    }
  }

  const current = opts.currentVersion || currentVersion();
  if (compareSemver(latest, current) <= 0) return;

  const warn = opts.warn || console.error;
  warn(`[pippit-cli] New version available: ${current} -> ${latest}. Run: pippit-cli update`);
}

module.exports = {
  CHECK_INTERVAL_MS,
  compareSemver,
  maybeWarnNewVersion,
  parseSemver,
};

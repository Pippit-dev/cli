const fs = require("fs");
const os = require("os");
const path = require("path");
const { runSilent } = require("./platform");
const { DEFAULT_PKG } = require("./skills");

const CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;

function defaultCacheFile() {
  return path.join(os.homedir(), ".pippit_tool_cli", "version-check.json");
}

function currentVersion() {
  return require("../package.json").version;
}

function parseSemver(version) {
  const match = String(version || "").trim().match(
    /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/
  );
  if (!match) return null;
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4] ? match[4].split(".") : [],
  };
}

function compareSemver(a, b) {
  const parsedA = parseSemver(a);
  const parsedB = parseSemver(b);
  if (!parsedA || !parsedB) return 0;
  for (const key of ["major", "minor", "patch"]) {
    const diff = parsedA[key] - parsedB[key];
    if (diff !== 0) return diff;
  }

  if (parsedA.prerelease.length === 0 || parsedB.prerelease.length === 0) {
    if (parsedA.prerelease.length === parsedB.prerelease.length) return 0;
    return parsedA.prerelease.length === 0 ? 1 : -1;
  }
  const count = Math.max(parsedA.prerelease.length, parsedB.prerelease.length);
  for (let i = 0; i < count; i++) {
    const identifierA = parsedA.prerelease[i];
    const identifierB = parsedB.prerelease[i];
    if (identifierA === undefined) return -1;
    if (identifierB === undefined) return 1;
    if (identifierA === identifierB) continue;
    const numericA = /^\d+$/.test(identifierA);
    const numericB = /^\d+$/.test(identifierB);
    if (numericA && numericB) return Number(identifierA) - Number(identifierB);
    if (numericA !== numericB) return numericA ? -1 : 1;
    return identifierA < identifierB ? -1 : 1;
  }
  return 0;
}

function distTagForVersion(version) {
  const parsed = parseSemver(version);
  return parsed && parsed.prerelease[0] === "beta" ? "beta" : "latest";
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

function fetchLatestVersion(pkg = DEFAULT_PKG, distTag = "latest") {
  return runSilent("npm", ["view", `${pkg}@${distTag}`, "version"], { timeout: 3000 }).toString().trim();
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
  const current = opts.currentVersion || currentVersion();
  const channel = distTagForVersion(current);
  const cacheFresh = cache && cache.channel === channel && now - cache.checkedAt < CHECK_INTERVAL_MS;

  let latest = cacheFresh ? cache.latest : "";
  if (!latest) {
    try {
      latest = (opts.fetchLatestVersion || fetchLatestVersion)(opts.pkg || DEFAULT_PKG, channel);
      writeCache(cacheFile, { channel, latest, checkedAt: now });
    } catch (_) {
      return;
    }
  }

  if (compareSemver(latest, current) <= 0) return;

  const warn = opts.warn || console.error;
  const updateCommand = channel === "beta"
    ? `npx ${opts.pkg || DEFAULT_PKG}@beta install`
    : "pippit-tool-cli update";
  warn(`[pippit-tool-cli] New version available: ${current} -> ${latest}. Run: ${updateCommand}`);
}

module.exports = {
  CHECK_INTERVAL_MS,
  compareSemver,
  defaultCacheFile,
  distTagForVersion,
  fetchLatestVersion,
  maybeWarnNewVersion,
  parseSemver,
};

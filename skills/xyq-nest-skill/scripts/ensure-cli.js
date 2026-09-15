#!/usr/bin/env node

// Self-contained entrypoint shipped inside the Skill ZIP. No npm dependencies.
const { execFileSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const REQUIRED_COMMANDS = [
  "login", "submit-run", "upload-file", "download-result", "query-result",
  "generate-image", "generate-video", "video-super-resolution",
  "erase-video-subtitle", "get-credit-balance",
];

function npmCommand() {
  if (process.platform !== "win32") return ["npm", []];
  // Run npm's JS entrypoint directly, avoiding cmd.exe quoting for paths with spaces.
  const dirs = [path.dirname(process.execPath), ...(process.env.PATH || "").split(path.delimiter)];
  for (const dir of dirs.filter(Boolean)) {
    const script = path.join(dir, "node_modules", "npm", "bin", "npm-cli.js");
    if (fs.existsSync(script)) return [process.execPath, [script]];
  }
  throw new Error("未找到 npm，请先安装 Node.js 16+ 和 npm。");
}

function findCLIOnPath() {
  for (const dir of (process.env.PATH || "").split(path.delimiter).filter(Boolean)) {
    const candidate = path.resolve(dir, `pippit-tool-cli${process.platform === "win32" ? ".exe" : ""}`);
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
      const resolved = fs.realpathSync(candidate);
      // Bypass the npm JS launcher, which may download a binary or check for updates.
      if (resolved.endsWith(path.join("scripts", "run.js"))) {
        const binary = path.resolve(path.dirname(resolved), "../bin/pippit-tool-cli");
        if (fs.existsSync(binary)) return binary;
      } else {
        return candidate;
      }
    }
    if (process.platform === "win32" && fs.existsSync(path.join(dir, "pippit-tool-cli.cmd"))) {
      const binary = path.resolve(dir, "node_modules/@pippit-dev/cli/bin/pippit-tool-cli.exe");
      if (fs.existsSync(binary)) return binary;
    }
  }
  return null;
}

function ensureCLI() {
  if (Number(process.versions.node.split(".")[0]) < 16) {
    throw new Error("需要 Node.js 16+ 和 npm。");
  }
  if (!["darwin", "linux", "win32"].includes(process.platform)
      || !["x64", "arm64"].includes(process.arch)) {
    throw new Error(`不支持的平台：${process.platform}-${process.arch}`);
  }
  const cacheDir = path.join(os.homedir(), ".cache", "pippit-tool-cli", "xyq-skill", `${process.platform}-${process.arch}`);
  const installedDir = path.join(cacheDir, "current");
  const binaryRelative = path.join("node_modules", "@pippit-dev", "cli", "bin", `pippit-tool-cli${process.platform === "win32" ? ".exe" : ""}`);
  const cachedCLI = path.join(installedDir, binaryRelative);
  const env = { ...process.env };
  delete env.XYQ_ACCESS_KEY;
  env.PIPPIT_CLI_DISABLE_UPDATE_CHECK = "1";
  let installDir;

  function run(command, args, label, quiet = false) {
    try {
      return execFileSync(command, args, {
        cwd: installDir, env, timeout: quiet ? 10000 : 180000,
        stdio: quiet ? ["ignore", "pipe", "pipe"] : ["ignore", 2, 2],
      });
    } catch (err) {
      const failure = new Error(`${label}失败（${err.code || err.status || "unknown"}），请检查安装、命令支持情况和网络。`);
      failure.exitStatus = err.status;
      throw failure;
    }
  }

  function checkCLI(cliPath, expectedVersion) {
    const version = run(cliPath, ["--version"], "检查 CLI 版本", true).toString().trim();
    if (expectedVersion && version !== expectedVersion) {
      throw new Error(`CLI 版本 ${version} 与 npm 包版本 ${expectedVersion} 不一致。`);
    }
    for (const command of REQUIRED_COMMANDS) {
      try {
        run(cliPath, [command, "--help"], `检查 ${command} 命令`, true);
      } catch (err) {
        if (Number.isInteger(err.exitStatus) && err.exitStatus !== 0) {
          err.missingCommand = command;
        }
        throw err;
      }
    }
    return { cli_path: cliPath, version };
  }

  const candidates = new Set([findCLIOnPath(), fs.existsSync(cachedCLI) ? cachedCLI : null]);
  for (const candidate of candidates) {
    if (!candidate) continue;
    try {
      return checkCLI(candidate);
    } catch (err) {
      if (!err.missingCommand) throw err;
      console.error(`已有 CLI 的 ${err.missingCommand} 命令检查未通过；继续检查缓存，无可用缓存时自动安装最新版本。`);
    }
  }

  const [npm, npmArgs] = npmCommand();
  fs.mkdirSync(cacheDir, { recursive: true });
  installDir = fs.mkdtempSync(path.join(cacheDir, "install-"));
  try {
    run(npm, [...npmArgs, "install", "--prefix", installDir,
      "--cache", path.join(installDir, ".npm-cache"), "--global=false", "--no-save",
      "--package-lock=false", "--ignore-scripts", "--prefer-online",
      "--no-audit", "--no-fund", "@pippit-dev/cli@latest"], "下载最新 npm 包");
    const packageDir = path.join(installDir, "node_modules", "@pippit-dev", "cli");
    const installer = path.join(packageDir, "scripts", "install-cli.js");
    if (!fs.existsSync(installer)) {
      throw new Error("npm latest 尚未提供 scripts/install-cli.js，请先发布包含只安装 CLI 入口的版本。");
    }
    run(process.execPath, [installer], "下载并安装最新 CLI 二进制");
    const cliPath = path.join(packageDir, "bin", `pippit-tool-cli${process.platform === "win32" ? ".exe" : ""}`);
    const pkg = JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
    const result = checkCLI(cliPath, pkg.version);
    // Preserve the previous installation until the replacement passes all checks.
    fs.rmSync(installedDir, { recursive: true, force: true });
    fs.renameSync(installDir, installedDir);
    return { ...result, cli_path: cachedCLI };
  } catch (err) {
    fs.rmSync(installDir, { recursive: true, force: true });
    throw err;
  }
}

if (require.main === module) {
  if (process.argv.length === 3 && process.argv[2] === "--help") {
    console.log("Usage: node ensure-cli.js\n优先复用 PATH 或缓存中命令齐全的 CLI，不存在或缺少必需命令时安装 npm latest，成功输出 {cli_path, version} JSON。");
  } else if (process.argv.length !== 2) {
    console.error("不支持的参数。用法：node ensure-cli.js");
    process.exitCode = 1;
  } else {
    try {
      console.log(JSON.stringify(ensureCLI()));
    } catch (err) {
      console.error(err.message);
      process.exitCode = 1;
    }
  }
}

module.exports = { ensureCLI };

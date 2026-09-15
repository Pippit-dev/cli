const assert = require("assert");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");
const vm = require("vm");

const root = fs.mkdtempSync(path.join(os.tmpdir(), "xyq install test & "));
const repository = path.resolve(__dirname, "..");
const bootstrap = path.join(repository, "skills/xyq-nest-skill/scripts/ensure-cli.js");

function load(file, { modules = {}, process: overrides = {}, dirname, main = false } = {}) {
  const module = { exports: {} };
  const output = [];
  const errors = [];
  const proc = {
    platform: process.platform, arch: process.arch, versions: process.versions,
    execPath: process.execPath, env: {}, argv: [process.execPath, file], exitCode: 0,
    ...overrides,
  };
  const req = (name) => Object.prototype.hasOwnProperty.call(modules, name) ? modules[name] : require(name);
  req.main = main ? module : null;
  vm.runInNewContext(fs.readFileSync(file, "utf8"), {
    require: req, module, process: proc, Buffer,
    __dirname: dirname || path.dirname(file),
    console: { log: (s) => output.push(s), warn: (s) => errors.push(s), error: (s) => errors.push(s) },
  }, { filename: file });
  return { api: module.exports, proc, output, errors };
}

function checkInstaller() {
  const helpPath = path.join(repository, "scripts/install-cli.js");
  const help = load(helpPath, {
    main: true,
    process: { argv: [process.execPath, helpPath, "--help"] },
    modules: { "./install": { install: () => assert.fail("Help must not install anything") } },
  });
  assert.strictEqual(help.proc.exitCode, 0);
  const packageDir = path.join(root, "package");
  fs.mkdirSync(packageDir);
  const effects = [];
  const archive = Buffer.from("verified archive fixture");
  const mockPlatform = {
    isWindows: process.platform === "win32",
    run(command, args, opts) {
      if (command === "curl") {
        fs.writeFileSync(args[args.indexOf("--output") + 1], archive);
      } else if (command === "tar" || command === "powershell.exe") {
        const dest = command === "tar" ? args[args.indexOf("-C") + 1] : opts.env.PIPPIT_CLI_DEST;
        fs.writeFileSync(path.join(dest, `pippit-tool-cli${mockPlatform.isWindows ? ".exe" : ""}`), "binary fixture");
      } else {
        assert.fail(`Unexpected external command: ${command}`);
      }
    },
  };
  const installer = load(path.join(repository, "scripts/install.js"), {
    dirname: path.join(packageDir, "scripts"),
    modules: {
      "../package.json": { version: "9.9.9" },
      "./platform": mockPlatform,
      "./skills": {
        installSkillsFromRoot: () => effects.push("install-skills"),
        cleanupLegacyGlobalSkills: () => effects.push("cleanup-skills"),
      },
      "./telemetry": { reportBundledSkillTelemetry: () => effects.push("telemetry") },
    },
  });
  const hash = crypto.createHash("sha256").update(archive).digest("hex");
  const checksumFile = path.join(packageDir, "checksums.txt");
  fs.writeFileSync(checksumFile, `${hash}  ${installer.api.archiveName}\n`);
  for (const skipSkills of [undefined, "1"]) {
    installer.proc.env.PIPPIT_CLI_SKIP_SKILLS = skipSkills;
    const entry = load(path.join(repository, "scripts/install-cli.js"), {
      main: true, modules: { "./install": installer.api },
    });
    assert.strictEqual(entry.proc.exitCode, 0);
    assert.deepStrictEqual(effects, [], "CLI-only entry must not touch Skills or telemetry");
    assert.strictEqual(fs.readFileSync(path.join(packageDir, "bin", `pippit-tool-cli${mockPlatform.isWindows ? ".exe" : ""}`), "utf8"), "binary fixture");
  }

  // Preserve the existing installer behavior for normal npm installs.
  delete installer.proc.env.PIPPIT_CLI_SKIP_SKILLS;
  installer.api.install();
  assert.deepStrictEqual(effects.splice(0), ["install-skills", "telemetry"]);
  installer.proc.env.PIPPIT_CLI_SKIP_SKILLS = "1";
  installer.api.install();
  assert.deepStrictEqual(effects.splice(0), ["cleanup-skills"]);

  fs.writeFileSync(checksumFile, `${"0".repeat(64)}  ${installer.api.archiveName}\n`);
  const failed = load(path.join(repository, "scripts/install-cli.js"), {
    main: true, modules: { "./install": installer.api },
  });
  assert.strictEqual(failed.proc.exitCode, 1);
  assert(failed.errors[0].includes("Checksum mismatch"));
  assert.deepStrictEqual(effects, []);
}

function bootstrapFixture({ failure, platform = "linux", version = "9.9.9" } = {}) {
  const dirs = [];
  const calls = [];
  const home = fs.mkdtempSync(path.join(root, "user-"));
  const npmDir = path.join(home, "node installation");
  const nodePath = path.join(npmDir, platform === "win32" ? "node.exe" : "node");
  const npmScript = path.join(npmDir, "node_modules/npm/bin/npm-cli.js");
  fs.mkdirSync(path.dirname(npmScript), { recursive: true });
  fs.writeFileSync(npmScript, "npm fixture");
  return {
    dirs, calls, npmDir,
    options: {
      main: true,
      process: { platform, execPath: nodePath, env: { XYQ_ACCESS_KEY: "test-key", PATH: npmDir } },
      modules: {
        os: { homedir: () => home },
        child_process: {
          execFileSync(command, args, options) {
            calls.push({ command, args, options });
            assert.strictEqual(options.env.XYQ_ACCESS_KEY, undefined);
            if (args.includes("@pippit-dev/cli@latest")) {
              const dir = args[args.indexOf("--prefix") + 1];
              dirs.push(dir);
              assert(args.includes("--ignore-scripts"));
              assert(args.includes("--global=false"));
              assert(args.includes("--prefer-online"));
              assert.strictEqual(args[args.indexOf("--cache") + 1], path.join(dir, ".npm-cache"));
              assert.strictEqual(options.cwd, dir);
              if (platform === "win32") {
                assert.strictEqual(command, nodePath);
                assert.strictEqual(args[0], npmScript);
              } else {
                assert.strictEqual(command, "npm");
              }
              if (failure === "npm") throw Object.assign(new Error("missing npm"), { code: "ENOENT" });
              const pkg = path.join(dir, "node_modules/@pippit-dev/cli");
              fs.mkdirSync(path.join(pkg, "scripts"), { recursive: true });
              fs.writeFileSync(path.join(pkg, "package.json"), JSON.stringify({ version }));
              if (failure !== "missing-installer") fs.writeFileSync(path.join(pkg, "scripts/install-cli.js"), "fixture");
            } else if (args[0].endsWith("install-cli.js")) {
              assert.strictEqual(command, nodePath);
              if (failure === "download") throw Object.assign(new Error("network"), { status: 1 });
              const bin = path.resolve(path.dirname(args[0]), "../bin");
              fs.mkdirSync(bin, { recursive: true });
              fs.writeFileSync(path.join(bin, platform === "win32" ? "pippit-tool-cli.exe" : "pippit-tool-cli"), "binary fixture");
            } else if (args[0] === "--version") {
              assert(path.isAbsolute(command));
              return Buffer.from(failure === "version" ? "0.0.1\n" : `${version}\n`);
            } else {
              assert.strictEqual(args[1], "--help");
              if (args[0] === "upload-file" && fs.existsSync(command)
                  && fs.readFileSync(command, "utf8").includes("missing-command")) {
                throw Object.assign(new Error("unknown command"), { status: 1 });
              }
              if (failure === args[0]) throw Object.assign(new Error("unknown command"), { status: 1 });
            }
            return Buffer.from("");
          },
        },
      },
    },
  };
}

function checkBootstrap() {
  for (const platform of ["linux", "darwin", "win32"]) {
    const fixture = bootstrapFixture({ platform });
    const first = load(bootstrap, fixture.options);
    const next = load(bootstrap, fixture.options);
    assert.strictEqual(first.proc.exitCode, 0, first.errors.join("\n"));
    assert.strictEqual(next.proc.exitCode, 0, next.errors.join("\n"));
    const result = JSON.parse(first.output[0]);
    assert.strictEqual(result.version, "9.9.9");
    assert(path.isAbsolute(result.cli_path));
    assert(result.cli_path.endsWith(platform === "win32" ? "pippit-tool-cli.exe" : "pippit-tool-cli"));
    assert.strictEqual(result.cli_path, JSON.parse(next.output[0]).cli_path);
    assert.strictEqual(fixture.dirs.length, 1, "Subsequent invocations must reuse the cached CLI");
    for (const command of ["login", "submit-run", "upload-file", "download-result", "query-result",
      "generate-image", "generate-video", "video-super-resolution", "erase-video-subtitle", "get-credit-balance"]) {
      assert.strictEqual(fixture.calls.filter((call) => call.args[0] === command).length, 2);
    }
    assert.strictEqual(fixture.calls.filter((call) => call.args[0].endsWith("install-cli.js")).length, 1);
    // If the binary is removed, repair the incomplete cache with a fresh installation.
    fs.rmSync(result.cli_path);
    const repaired = load(bootstrap, fixture.options);
    assert.strictEqual(repaired.proc.exitCode, 0, repaired.errors.join("\n"));
    assert.strictEqual(fixture.dirs.length, 2);
  }
  for (const platform of ["linux", "win32"]) {
    for (const missingCommand of [false, true]) {
      const fixture = bootstrapFixture({ platform });
      const existing = path.join(fixture.npmDir, platform === "win32" ? "pippit-tool-cli.exe" : "pippit-tool-cli");
      const original = missingCommand ? "missing-command" : "existing CLI fixture";
      fs.writeFileSync(existing, original);
      // A complete existing CLI must work without npm. Missing commands trigger one upgrade.
      if (!missingCommand) fs.rmSync(path.join(fixture.npmDir, "node_modules/npm/bin/npm-cli.js"));
      const result = load(bootstrap, fixture.options);
      assert.strictEqual(result.proc.exitCode, 0, result.errors.join("\n"));
      if (!missingCommand) assert.strictEqual(JSON.parse(result.output[0]).cli_path, existing);
      else assert.notStrictEqual(JSON.parse(result.output[0]).cli_path, existing);
      assert.strictEqual(fixture.dirs.length, missingCommand ? 1 : 0);
      assert.strictEqual(fs.readFileSync(existing, "utf8"), original);
      const next = load(bootstrap, fixture.options);
      assert.strictEqual(next.proc.exitCode, 0, next.errors.join("\n"));
      assert.strictEqual(fixture.dirs.length, missingCommand ? 1 : 0, "An old PATH CLI must not cause repeated upgrades when cache is usable");
    }
  }
  const outdatedCache = bootstrapFixture();
  const initial = load(bootstrap, outdatedCache.options);
  const cachedPath = JSON.parse(initial.output[0]).cli_path;
  fs.writeFileSync(cachedPath, "missing-command");
  const upgraded = load(bootstrap, outdatedCache.options);
  assert.strictEqual(upgraded.proc.exitCode, 0, upgraded.errors.join("\n"));
  assert.strictEqual(fs.readFileSync(cachedPath, "utf8"), "binary fixture");
  assert.strictEqual(outdatedCache.dirs.length, 2);
  assert.strictEqual(load(bootstrap, outdatedCache.options).proc.exitCode, 0);
  assert.strictEqual(outdatedCache.dirs.length, 2);

  fs.writeFileSync(cachedPath, "missing-command");
  const execute = outdatedCache.options.modules.child_process.execFileSync;
  outdatedCache.options.modules.child_process.execFileSync = (command, args, options) => {
    if (args[0].endsWith("install-cli.js")) throw Object.assign(new Error("download failed"), { status: 1 });
    return execute(command, args, options);
  };
  assert.strictEqual(load(bootstrap, outdatedCache.options).proc.exitCode, 1);
  assert.strictEqual(fs.readFileSync(cachedPath, "utf8"), "missing-command", "Failed upgrades must preserve the original cache");

  const failedUpgrade = bootstrapFixture({ failure: "upload-file" });
  const oldPath = path.join(failedUpgrade.npmDir, "pippit-tool-cli");
  fs.writeFileSync(oldPath, "missing-command");
  assert.strictEqual(load(bootstrap, failedUpgrade.options).proc.exitCode, 1);
  assert.strictEqual(failedUpgrade.dirs.length, 1, "An incompatible latest release must fail without an upgrade loop");
  assert.strictEqual(fs.readFileSync(oldPath, "utf8"), "missing-command");
  for (const failure of ["npm", "missing-installer", "download", "version", "upload-file"]) {
    const fixture = bootstrapFixture({ failure });
    const result = load(bootstrap, fixture.options);
    assert.strictEqual(result.proc.exitCode, 1, failure);
    assert.strictEqual(result.output.length, 0, "Failure must not return a usable CLI path");
    assert(result.errors.length > 0);
    for (const dir of fixture.dirs) assert.strictEqual(fs.existsSync(dir), false);
  }
  const help = bootstrapFixture();
  help.options.process.argv = [process.execPath, bootstrap, "--help"];
  assert.strictEqual(load(bootstrap, help.options).proc.exitCode, 0);
  assert.strictEqual(help.calls.length, 0, "Help must not install or download anything");
}

try {
  checkInstaller();
  checkBootstrap();
  const pkg = require("../package.json");
  assert(pkg.files.includes("scripts/install-cli.js"), "npm package must ship the CLI-only entry");
  console.log("CLI-only installer and Skill bootstrap checks passed");
} finally {
  fs.rmSync(root, { recursive: true, force: true });
}

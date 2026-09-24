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
    require: req, module, process: proc, Buffer, URL,
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
  const payloads = [];
  const environment = { CODEX_THREAD_ID: 'private-id' };
  const telemetry = load(path.join(repository, "scripts/telemetry.js"), {
    process: { env: environment },
    modules: {
      "../package.json": { version: "9.9.9" },
      http: { request: () => ({ on() {}, end: body => payloads.push(JSON.parse(body)) }) },
      https: { request: () => ({ on() {}, end: body => payloads.push(JSON.parse(body)) }) },
    },
  });
  const archive = Buffer.from("verified archive fixture");
  const mockPlatform = {
    isWindows: process.platform === "win32",
    run(command, args, opts) {
      if (command === "curl") {
        assert(args.includes("--progress-bar"));
        assert(!args.includes("--silent"));
        assert.strictEqual(args[args.indexOf("--max-time") + 1], "600");
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
    process: { env: environment },
    modules: {
      "../package.json": { version: "9.9.9" },
      "./platform": mockPlatform,
      "./skills": {
        installSkillsFromRoot: () => effects.push("install-skills"),
        cleanupLegacyGlobalSkills: () => effects.push("cleanup-skills"),
      },
      "./telemetry": { reportBundledSkillTelemetry: (...args) => {
        effects.push("telemetry");
        telemetry.api.reportBundledSkillTelemetry(...args);
      } },
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
  assert.strictEqual(payloads.length, 2);
  for (const payload of payloads.splice(0)) {
    assert.strictEqual(payload.host_platform, "codex");
    assert.strictEqual(payload.source, "npm_install");
    assert.strictEqual(payload.event, "install");
    assert.strictEqual(payload.platform, process.platform);
    assert(!JSON.stringify(payload).includes("private-id"));
  }
  installer.proc.env.PIPPIT_CLI_SKIP_SKILLS = "1";
  installer.api.install();
  assert.deepStrictEqual(effects.splice(0), ["cleanup-skills"]);
  assert.strictEqual(payloads.length, 0);

  fs.writeFileSync(checksumFile, `${"0".repeat(64)}  ${installer.api.archiveName}\n`);
  const failed = load(path.join(repository, "scripts/install-cli.js"), {
    main: true, modules: { "./install": installer.api },
  });
  assert.strictEqual(failed.proc.exitCode, 1);
  assert(failed.errors[0].includes("Checksum mismatch"));
  assert.deepStrictEqual(effects, []);
}

function bootstrapFixture({ failure, platform = "linux", version = "9.9.9", missingCommand = "generate-image", canvas = false } = {}) {
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
      process: { platform, execPath: nodePath, argv: [nodePath, bootstrap, ...(canvas ? ["--canvas"] : [])], env: { XYQ_ACCESS_KEY: "test-key", PATH: npmDir } },
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
              if (failure !== "missing-canvas-entry") fs.writeFileSync(path.join(pkg, "scripts/run.js"), "fixture");
            } else if (args[0].endsWith("install-cli.js")) {
              assert.strictEqual(command, nodePath);
              if (failure === "download") throw Object.assign(new Error("network"), { status: 1 });
              const bin = path.resolve(path.dirname(args[0]), "../bin");
              fs.mkdirSync(bin, { recursive: true });
              fs.writeFileSync(path.join(bin, platform === "win32" ? "pippit-tool-cli.exe" : "pippit-tool-cli"), "binary fixture");
            } else if (args[0].endsWith("run.js")) {
              assert.strictEqual(command, nodePath);
              assert.deepStrictEqual(Array.from(args.slice(1)), ["canvas", "command", "list"]);
              if (failure === "canvas-runtime") throw Object.assign(new Error("runtime missing"), { status: 1 });
              if (failure === "canvas-json") return Buffer.from("invalid JSON");
              const commands = failure === "canvas-catalog" ? [] : [{ name: "get_snapshot" }, { name: "create_biz_node" }];
              return Buffer.from(JSON.stringify({ commands }));
            } else if (args[0] === "--version") {
              assert(path.isAbsolute(command));
              return Buffer.from(failure === "version" ? "0.0.1\n" : `${version}\n`);
            } else {
              assert.strictEqual(args[args.length - 1], "--help");
              const commandName = args.slice(0, -1).join(" ");
              if (commandName === missingCommand && fs.existsSync(command)
                  && fs.readFileSync(command, "utf8").includes("missing-command")) {
                throw Object.assign(new Error("unknown command"), { status: 1 });
              }
              if (failure === commandName) throw Object.assign(new Error("unknown command"), { status: 1 });
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
    for (const command of ["status", "login", "logout", "query-result",
      "generate-image", "generate-video", "video-super-resolution", "erase-video-subtitle", "get-credit-balance", "model list", "model describe"]) {
      assert.strictEqual(fixture.calls.filter((call) => call.args.slice(0, -1).join(" ") === command).length, 2);
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
  // Every retained command participates in compatibility checks and cache reuse.
  for (const missingCommand of ["status", "login", "logout", "query-result", "generate-image",
    "generate-video", "video-super-resolution", "erase-video-subtitle", "get-credit-balance", "model list", "model describe"]) {
    const fixture = bootstrapFixture({ missingCommand });
    const existing = path.join(fixture.npmDir, "pippit-tool-cli");
    fs.writeFileSync(existing, "missing-command");
    const upgraded = load(bootstrap, fixture.options);
    assert.strictEqual(upgraded.proc.exitCode, 0, upgraded.errors.join("\n"));
    assert.notStrictEqual(JSON.parse(upgraded.output[0]).cli_path, existing);
    assert.strictEqual(fixture.dirs.length, 1);
    assert.strictEqual(load(bootstrap, fixture.options).proc.exitCode, 0);
    assert.strictEqual(fixture.dirs.length, 1, `Reuse cache after upgrading ${missingCommand}`);
  }
  // Removed Skill commands must not force an otherwise usable CLI to upgrade.
  for (const missingCommand of ["submit-run", "get-thread", "upload-file", "download-result"]) {
    const fixture = bootstrapFixture({ missingCommand });
    const existing = path.join(fixture.npmDir, "pippit-tool-cli");
    fs.writeFileSync(existing, "missing-command");
    const reused = load(bootstrap, fixture.options);
    assert.strictEqual(reused.proc.exitCode, 0, reused.errors.join("\n"));
    assert.strictEqual(JSON.parse(reused.output[0]).cli_path, existing);
    assert.strictEqual(fixture.dirs.length, 0);
    assert(!fixture.calls.some((call) => call.args[0] === missingCommand));
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

  const failedUpgrade = bootstrapFixture({ failure: "generate-image" });
  const oldPath = path.join(failedUpgrade.npmDir, "pippit-tool-cli");
  fs.writeFileSync(oldPath, "missing-command");
  assert.strictEqual(load(bootstrap, failedUpgrade.options).proc.exitCode, 1);
  assert.strictEqual(failedUpgrade.dirs.length, 1, "An incompatible latest release must fail without an upgrade loop");
  assert.strictEqual(fs.readFileSync(oldPath, "utf8"), "missing-command");
  for (const failure of ["npm", "missing-installer", "download", "version", "status", "logout", "generate-image", "query-result", "model list", "model describe"]) {
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

function checkCanvasBootstrap() {
  for (const platform of ["darwin", "linux", "win32"]) {
    const fixture = bootstrapFixture({ platform, canvas: true });
    const first = load(bootstrap, fixture.options);
    assert.strictEqual(first.proc.exitCode, 0, first.errors.join("\n"));
    const result = JSON.parse(first.output[0]);
    assert(path.isAbsolute(result.canvas_entry));
    assert(fs.existsSync(result.canvas_entry), "Return the relocated npm entry, not the temporary install path");
    const next = load(bootstrap, fixture.options);
    assert.strictEqual(next.proc.exitCode, 0, next.errors.join("\n"));
    assert.strictEqual(JSON.parse(next.output[0]).canvas_entry, result.canvas_entry);
    assert.strictEqual(fixture.dirs.length, 1);
    for (const command of ["create", "get", "allocate", "upload", "apply"]) {
      assert.strictEqual(fixture.calls.filter((call) => call.args.join(" ") === `canvas ${command} --help`).length, 2);
    }
    assert.strictEqual(fixture.calls.filter((call) => call.args[0].endsWith("run.js")).length, 2);

    // Removing only the runtime entry upgrades Canvas, while normal media use still reuses the binary.
    fs.rmSync(result.canvas_entry);
    const mediaOptions = { ...fixture.options, process: { ...fixture.options.process, argv: ["node", bootstrap] } };
    assert.strictEqual(load(bootstrap, mediaOptions).proc.exitCode, 0);
    assert.strictEqual(fixture.dirs.length, 1);
    const repaired = load(bootstrap, fixture.options);
    assert.strictEqual(repaired.proc.exitCode, 0, repaired.errors.join("\n"));
    assert(fs.existsSync(JSON.parse(repaired.output[0]).canvas_entry));
    assert.strictEqual(fixture.dirs.length, 2);

    // A complete npm package found on PATH must not be reinstalled.
    fixture.options.process.env.PATH = path.dirname(result.cli_path);
    assert.strictEqual(load(bootstrap, fixture.options).proc.exitCode, 0);
    assert.strictEqual(fixture.dirs.length, 2);
  }
  const standalone = bootstrapFixture({ canvas: true });
  const nativePath = path.join(standalone.npmDir, "pippit-tool-cli");
  fs.writeFileSync(nativePath, "existing standalone binary");
  const upgraded = load(bootstrap, standalone.options);
  assert.strictEqual(upgraded.proc.exitCode, 0, upgraded.errors.join("\n"));
  assert.notStrictEqual(JSON.parse(upgraded.output[0]).cli_path, nativePath);
  assert.strictEqual(standalone.dirs.length, 1);
  assert.strictEqual(fs.readFileSync(nativePath, "utf8"), "existing standalone binary");
  assert.strictEqual(load(bootstrap, standalone.options).proc.exitCode, 0);
  assert.strictEqual(standalone.dirs.length, 1);

  for (const failure of ["missing-canvas-entry", "canvas-runtime", "canvas-json", "canvas-catalog", "canvas get"]) {
    const fixture = bootstrapFixture({ canvas: true, failure });
    const failed = load(bootstrap, fixture.options);
    assert.strictEqual(failed.proc.exitCode, 1, failure);
    assert.strictEqual(fixture.dirs.length, 1, "An incompatible latest package must stop after one attempt");
    assert.strictEqual(failed.output.length, 0);
  }

  const failure = bootstrapFixture({ canvas: true });
  const initial = load(bootstrap, failure.options);
  const original = JSON.parse(initial.output[0]);
  const execute = failure.options.modules.child_process.execFileSync;
  failure.options.modules.child_process.execFileSync = (command, args, options) => {
    if (args[0].endsWith("run.js")) throw Object.assign(new Error("timeout"), { status: null });
    return execute(command, args, options);
  };
  assert.strictEqual(load(bootstrap, failure.options).proc.exitCode, 1);
  assert.strictEqual(failure.dirs.length, 1, "Runtime timeouts must not trigger an upgrade");
  failure.options.modules.child_process.execFileSync = (command, args, options) => {
    if (args[0].endsWith("run.js")) throw Object.assign(new Error("invalid runtime"), { status: 1 });
    return execute(command, args, options);
  };
  assert.strictEqual(load(bootstrap, failure.options).proc.exitCode, 1);
  assert.strictEqual(failure.dirs.length, 2);
  assert(fs.existsSync(original.canvas_entry), "Failed Canvas upgrade must preserve the previous npm package");
  assert(fs.existsSync(original.cli_path));
}

try {
  checkInstaller();
  checkBootstrap();
  checkCanvasBootstrap();
  const pkg = require("../package.json");
  assert(pkg.files.includes("scripts/install-cli.js"), "npm package must ship the CLI-only entry");
  console.log("CLI-only installer and Skill bootstrap checks passed");
} finally {
  fs.rmSync(root, { recursive: true, force: true });
}

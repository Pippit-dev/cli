'use strict';

const assert = require('assert');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const root = path.resolve(__dirname, '..');
const fixture = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'pippit-cli-help-')));
const scripts = path.join(fixture, 'scripts');
const native = path.join(
  fixture,
  'bin',
  `pippit-tool-cli${process.platform === 'win32' ? '.exe' : ''}`,
);
const windowsNative = path.join(fixture, 'bin', 'pippit-tool-cli.exe');
let checks = 0;

try {
  fs.mkdirSync(scripts);
  fs.mkdirSync(path.dirname(native));
  fs.writeFileSync(path.join(fixture, 'package.json'), JSON.stringify(require('../package.json')));
  for (const name of ['run.js', 'install-wizard.js']) {
    fs.copyFileSync(path.join(__dirname, name), path.join(scripts, name));
  }
  for (const name of ['canvas-command', 'version-check', 'platform', 'skills', 'telemetry']) {
    fs.writeFileSync(
      path.join(scripts, `${name}.js`),
      `module.exports = require(${JSON.stringify(path.join(__dirname, name))});\n`,
    );
  }

  // Child processes run the actual entrypoints. Only the compiled native CLI may
  // be spawned; an attempted npm/installer/network/cache write fails the check,
  // even if the production code catches the thrown error.
  const guard = path.join(fixture, 'read-only-guard.cjs');
  fs.writeFileSync(
    guard,
    `
const childProcess = require("child_process");
const fs = require("fs");
if (process.env.PIPPIT_TEST_WINDOWS_HELP === "1") {
  Object.defineProperty(process, "platform", { value: "win32" });
}
const deny = (name) => () => {
  process.stderr.write("UNEXPECTED_SIDE_EFFECT:" + name + "\\n");
  throw new Error("Help attempted a side effect: " + name);
};
const execFileSync = childProcess.execFileSync;
let nativeInvocations = 0;
childProcess.execFileSync = (command, args, options) => {
  if (![${JSON.stringify(native)}, ${JSON.stringify(windowsNative)}].includes(command)) return deny("execFileSync")();
  if (++nativeInvocations > 1) return deny("repeated native preparation")();
  return execFileSync(command, args, options);
};
for (const method of ["exec", "execSync", "execFile", "spawn", "spawnSync", "fork"]) childProcess[method] = deny(method);
for (const method of ["writeFileSync", "appendFileSync", "mkdirSync", "rmSync", "renameSync", "unlinkSync"]) fs[method] = deny(method);
require("net").Socket.prototype.connect = deny("socket.connect");
for (const protocol of ["http", "https"]) {
  require(protocol).request = deny(protocol + ".request");
  require(protocol).get = deny(protocol + ".get");
}
globalThis.fetch = deny("fetch");
`,
  );
  const env = { ...process.env, NODE_OPTIONS: `--require=${JSON.stringify(guard)}` };
  delete env.CI;
  delete env.PIPPIT_CLI_DISABLE_UPDATE_CHECK;
  delete env.PIPPIT_CLI_DISABLE_TELEMETRY;

  function launch(script, args, status = 0, extraEnv = {}, scriptDirectory = scripts) {
    const result = spawnSync(process.execPath, [path.join(scriptDirectory, script), ...args], {
      encoding: 'utf8',
      env: { ...env, ...extraEnv },
      timeout: 15000,
    });
    assert.ifError(result.error);
    assert.strictEqual(result.status, status, `${script} ${args.join(' ')}: ${result.stderr}`);
    assert(!result.stderr.includes('UNEXPECTED_SIDE_EFFECT'), result.stderr);
    checks += 1;
    return result;
  }

  // No native binary exists: help must not attempt to prepare or install one.
  for (const flag of ['--help', '-h']) {
    for (const [script, args] of [
      ['run.js', ['install', flag]],
      ['install-wizard.js', [flag]],
    ]) {
      const result = launch(script, args);
      assert(result.stdout.includes('pippit-tool-cli install'));
      assert(result.stdout.includes('without installing'));
    }
    const result = launch('run.js', ['canvas', 'command', flag]);
    assert(result.stdout.includes('canvas command list'));
    const runHelp = launch('run.js', ['canvas', 'command', 'run', 'example.command', flag]);
    assert(runHelp.stdout.includes('canvas command run'));
  }
  const unknownInstall = launch('run.js', ['install', 'unknown-argument'], 1);
  assert(unknownInstall.stderr.includes('install --help'));
  const unknownAction = launch('run.js', ['canvas', 'command', 'unknown-action'], 1);
  assert(unknownAction.stderr.length > 0);

  const build = spawnSync('go', ['build', '-o', native, '.'], {
    cwd: root,
    encoding: 'utf8',
    timeout: 180000,
  });
  assert.ifError(build.error);
  assert.strictEqual(build.status, 0, build.stderr);
  for (const args of [['--help'], ['canvas', '--help'], ['help', 'canvas']]) {
    const result = launch('run.js', args);
    for (const action of ['list', 'describe', 'schema', 'guide', 'run']) {
      assert(result.stdout.includes(`canvas command ${action}`), result.stdout);
    }
  }
  for (const action of ['list', 'run', 'unknown-action']) {
    const result = spawnSync(native, ['canvas', 'command', action], {
      encoding: 'utf8',
      timeout: 15000,
    });
    assert.ifError(result.error);
    assert.strictEqual(result.status, 1, result.stderr);
    assert(result.stderr.includes('npm launcher'), result.stderr);
    assert.strictEqual(result.stdout, '');
    checks += 1;
  }

  // Exercise the Windows launcher branch on every host using the same compiled
  // native fixture. An interrupted update must not be recovered by reading help.
  if (native !== windowsNative) fs.copyFileSync(native, windowsNative);
  const oldNative = `${windowsNative}.old`;
  const backup = Buffer.from('preserve the interrupted update backup');
  fs.writeFileSync(oldNative, backup);
  const windowsEnv = { PIPPIT_TEST_WINDOWS_HELP: '1' };
  const helpArgs = [[], ['--help'], ['-h'], ['canvas', '--help'], ['help', 'canvas']];
  for (const args of helpArgs) {
    const result = launch('run.js', args, 0, windowsEnv);
    assert(result.stdout.includes('canvas command list'));
  }
  assert.deepStrictEqual(fs.readFileSync(oldNative), backup);

  // Use an independent missing-binary fixture. Windows can retain a lock on
  // the executable just launched above, so renaming it is not a reliable way
  // to establish the missing-binary precondition.
  const missingRoot = path.join(fixture, 'missing-native');
  const missingScripts = path.join(missingRoot, 'scripts');
  const missingNative = path.join(missingRoot, 'bin', 'pippit-tool-cli.exe');
  const missingOld = `${missingNative}.old`;
  fs.mkdirSync(missingScripts, { recursive: true });
  fs.mkdirSync(path.dirname(missingNative));
  for (const name of fs.readdirSync(scripts)) {
    fs.copyFileSync(path.join(scripts, name), path.join(missingScripts, name));
  }
  fs.copyFileSync(path.join(fixture, 'package.json'), path.join(missingRoot, 'package.json'));
  fs.writeFileSync(missingOld, backup);
  for (const args of helpArgs) {
    const result = launch('run.js', args, 1, windowsEnv, missingScripts);
    assert(result.stderr.includes('Native CLI binary is missing'));
  }
  assert.strictEqual(fs.existsSync(missingNative), false);
  assert.deepStrictEqual(fs.readFileSync(missingOld), backup);
  assert.deepStrictEqual(fs.readFileSync(windowsNative), fs.readFileSync(native));
  assert.deepStrictEqual(fs.readFileSync(oldNative), backup);
  console.log(`Launcher help: ${checks} real child-process checks passed.`);
} finally {
  fs.rmSync(fixture, { recursive: true, force: true });
}

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
  if (process.env.PIPPIT_TEST_REJECT_NATIVE === "1") return deny("raw write native invocation")();
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

  const removedApplyArgs = [
      ['canvas', 'apply'], ['canvas', 'apply', '--help'],
      ['canvas', 'apply', '--file', '-', '--transport-result'],
      ['--team-id', '12', 'canvas', 'apply'], ['canvas', '_apply', '--file', '-'],
      ['canvas', '--team-id=0', 'apply'], ['canvas', '--team-id', '0', 'apply', '--file', '-'],
      ['--space-context', 'binding', 'canvas', '--team-id=0', 'apply'], ['help', 'canvas', 'apply'],
      ['canvas', '--project-id=123', 'apply'], ['canvas', '--project-id', '123', 'apply'],
      ['canvas', '--file', '-', 'apply'], ['canvas', '--file=-', 'apply'],
      ['--project-id=123', 'canvas', 'apply'], ['--file', '-', 'canvas', 'apply'],
      ['canvas', '--project-id=123', '--team-id', '12', 'apply'],
      ['canvas', '--team-id=12', '--file', '-', 'apply'],
      ['--team-id', '12', 'canvas', '--project-id', '123', '--file', '-', 'apply'],
      ['--space-context=binding', '--file=-', 'canvas', '--team-id=12', '--project-id=123', 'apply'],
      ['canvas', '--transport-result=true', '--file=-', 'apply'],
      ['canvas', '--transport-result=false', '--project-id=123', 'apply', '--help'],
      ['canvas', '--file', '-', '--team-id', '12', '_apply'],
      ['--project-id=123', 'canvas', '--file=-', '_apply'],
    ];
  function checkRemovedApply() {
    for (const args of removedApplyArgs) {
      const result = launch('run.js', args, 1, { PIPPIT_TEST_REJECT_NATIVE: '1', PIPPIT_CLI_CANVAS_TRANSPORT: '1' });
      assert(result.stderr.includes('use canvas command run'), result.stderr);
      assert.strictEqual(result.stdout, '');
    }
  }
  checkRemovedApply(); // Missing binary: no installation or update check.

  // Compile an offline legacy Cobra fixture with the old apply flag contract.
  // Prove the pre-command flags reach an executable old write entry, then run
  // the actual npm launcher against that binary with native dispatch forbidden.
  const legacySource = path.join(fixture, 'legacy.go');
  fs.writeFileSync(legacySource, `package main
import ("fmt"; "os"; "github.com/spf13/cobra")
func main() {
  root := &cobra.Command{Use: "pippit-tool-cli", SilenceErrors: true, SilenceUsage: true}
  root.PersistentFlags().String("team-id", "", "space")
  root.PersistentFlags().String("space-context", "", "binding")
  canvas := &cobra.Command{Use: "canvas", Args: cobra.NoArgs}
  apply := &cobra.Command{Use: "apply", Args: cobra.NoArgs, Run: func(*cobra.Command, []string) { fmt.Println("LEGACY_APPLY_DISPATCHED") }}
  apply.Flags().String("project-id", "", "project")
  apply.Flags().String("file", "-", "request")
  apply.Flags().Bool("transport-result", false, "transport")
  canvas.AddCommand(apply)
  root.AddCommand(canvas)
  if err := root.Execute(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
`);
  const legacyBuild = spawnSync('go', ['build', '-o', native, legacySource], { cwd: root, encoding: 'utf8', timeout: 180000 });
  assert.ifError(legacyBuild.error);
  assert.strictEqual(legacyBuild.status, 0, legacyBuild.stderr);
  for (const args of removedApplyArgs.filter((args) => !args.includes('_apply') && args[0] !== 'help' && !args.includes('--help'))) {
    const result = spawnSync(native, args, { encoding: 'utf8', timeout: 15000 });
    assert.ifError(result.error);
    assert.strictEqual(result.status, 0, `${args.join(' ')}: ${result.stderr}`);
    assert(result.stdout.includes('LEGACY_APPLY_DISPATCHED'), result.stdout);
    checks += 1;
  }
  checkRemovedApply(); // Legacy binary: no raw writes, installation or update.

  const build = spawnSync('go', ['build', '-o', native, '.'], {
    cwd: root,
    encoding: 'utf8',
    timeout: 180000,
  });
  assert.ifError(build.error);
  assert.strictEqual(build.status, 0, build.stderr);
  checkRemovedApply(); // Existing binary: never forward the removed entry.
  const ordinaryArgs = launch('run.js', ['canvas', 'create', '--title', 'canvas', 'apply', '--help']);
  assert(ordinaryArgs.stdout.includes('canvas create'), ordinaryArgs.stdout);
  for (const [args, status] of [
    [['generate-image', '--prompt', 'canvas', 'apply', '--help'], 0],
    [['--team-id', 'canvas', 'generate-image', '--prompt', 'apply', '--help'], 0],
    [['canvas', 'create', '--title', 'apply', '--help'], 0],
    [['canvas', '--project-id=canvas', 'create', '--title', 'apply', '--help'], 1],
    [['canvas', '--file', 'apply', '--help'], 1],
    [['canvas', '--', 'apply', '--help'], 1],
  ]) {
    const result = launch('run.js', args, status);
    assert(!result.stderr.includes('canvas apply is unavailable'), result.stderr);
    assert(result.stdout.length > 0 || result.stderr.length > 0);
  }
  const ordinaryCommandArgs = launch('run.js', ['canvas', 'command', 'describe', 'canvas', 'apply'], 1);
  assert(ordinaryCommandArgs.stderr.includes('多余的位置参数'), ordinaryCommandArgs.stderr);
  assert(!ordinaryCommandArgs.stderr.includes('canvas apply is unavailable'));
  for (const args of [['--help'], ['canvas', '--help'], ['help', 'canvas']]) {
    const result = launch('run.js', args);
    assert(!/^\s+(?:apply|_apply)\s/m.test(result.stdout), result.stdout);
    for (const action of ['list', 'describe', 'schema', 'guide', 'run']) {
      assert(result.stdout.includes(`canvas command ${action}`), result.stdout);
    }
  }
  for (const args of [['canvas', 'apply'], ['canvas', '--team-id=0', 'apply'], ['canvas', '_apply']]) {
    const result = spawnSync(native, args, { encoding: 'utf8', timeout: 15000, env: { ...process.env, PIPPIT_CLI_CANVAS_TRANSPORT: '' } });
    assert.ifError(result.error);
    assert.strictEqual(result.status, 1, result.stderr);
    assert.strictEqual(result.stdout, '');
    checks += 1;
  }
  const removedHelp = spawnSync(native, ['canvas', 'apply', '--help'], { encoding: 'utf8', timeout: 15000 });
  assert.ifError(removedHelp.error);
  assert.strictEqual(removedHelp.status, 0, removedHelp.stderr);
  assert(removedHelp.stdout.includes('canvas command list'), removedHelp.stdout);
  assert(!/^\s+(?:apply|_apply)\s/m.test(removedHelp.stdout), removedHelp.stdout);
  const completion = spawnSync(native, ['__complete', 'canvas', ''], { encoding: 'utf8', timeout: 15000 });
  assert.ifError(completion.error);
  assert.strictEqual(completion.status, 0, completion.stderr);
  assert(!/^(?:apply|_apply)(?:\t|$)/m.test(completion.stdout), completion.stdout);
  const bareHelp = spawnSync(native, ['canvas', '--team-id=invalid', '--space-context=invalid'], { encoding: 'utf8', timeout: 15000 });
  assert.ifError(bareHelp.error);
  assert.strictEqual(bareHelp.status, 0, bareHelp.stderr);
  assert.strictEqual(bareHelp.stderr, '');
  assert(bareHelp.stdout.includes('canvas command list'), bareHelp.stdout);
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

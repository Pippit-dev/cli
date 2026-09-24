const assert = require("assert");

const { defaultInstallPackage, installPackage } = require("./install-wizard");
const { DEFAULT_PKG } = require("./skills");

const version = require("../package.json").version.replace(/-.*$/, "");

delete process.env.PIPPIT_CLI_INSTALL_PACKAGE;
assert.strictEqual(defaultInstallPackage(), `${DEFAULT_PKG}@${version}`);
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@${version}`);

process.env.PIPPIT_CLI_INSTALL_PACKAGE = `${DEFAULT_PKG}@0.0.26`;
assert.strictEqual(installPackage(), `${DEFAULT_PKG}@0.0.26`);

// Exercise install attribution without npm, Skill writes, or network requests.
const fs = require('fs');
const vm = require('vm');
function installFixture(args, source, retry = false, environment = {}) {
  const reports = [], installs = [], payloads = [];
  const proc = { argv: [], env: { ...environment, ...(source === undefined ? {} : { PIPPIT_CLI_SOURCE: source }) } };
  const module = { exports: {} };
  let skillCalls = 0;
  const modules = {
    fs: { existsSync: () => true },
    './platform': {
      isWindows: false,
      run: (...args) => installs.push(args),
      runSilent: (_, args) => args[0] === 'list' ? '@pippit-dev/cli@1.0.0' : '/fixture',
    },
    './skills': { DEFAULT_PKG, installGlobalPackageSkills: () => {
      if (retry && skillCalls++ === 0) throw new Error('missing skills');
    } },
    './telemetry': { reportBundledSkillTelemetry: (event, source, host) => {
      loadTelemetry(proc.env, payloads).reportBundledSkillTelemetry(event, source, host);
      reports.push([event, source, payloads[0].host_platform || '']);
    } },
  };
  vm.runInNewContext(fs.readFileSync(require.resolve('./install-wizard'), 'utf8'), {
    module, process: proc, console: { log() {}, error() {} },
    require: name => modules[name] || require(name),
  });
  module.exports.main(args);
  return { reports, installs, proc, payloads };
}
for (const [args, env, want] of [
  [[], undefined, ''], [[], ' workbuddy ', 'workbuddy'],
  [['--source', ' doubao_office '], 'workbuddy', 'doubao_office'],
  [['--source=custom-host'], undefined, 'custom-host'],
  [['--source', ''], 'workbuddy', ''], [['--source=  '], 'workbuddy', ''],
]) {
  const result = installFixture(args, env, true);
  assert.deepStrictEqual(result.reports, [['install', 'npx_install', want]]);
  assert.strictEqual(result.installs.length, 2);
  assert(result.installs.every(call => call[2].env.PIPPIT_CLI_SKIP_SKILLS === '1'), 'nested postinstall must not double report');
}
for (const args of [['--help'], ['--source'], ['--unknown']]) {
  const result = installFixture(args);
  assert.strictEqual(result.installs.length, 0);
  assert.strictEqual(result.reports.length, 0);
}

// Verify the HTTP payload, including omission and telemetry opt-out.
function loadTelemetry(env, payloads) {
  const request = () => ({ on() {}, end: body => payloads.push(JSON.parse(body)) });
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(require.resolve('./telemetry'), 'utf8'), {
    module, URL, Buffer, console,
    process: { env },
    require: name => ['http', 'https'].includes(name) ? { request } : require(name),
  });
  return module.exports;
}
function telemetryFixture(source, disabled) {
  const payloads = [];
  loadTelemetry({ PIPPIT_CLI_DISABLE_TELEMETRY: disabled }, payloads)
    .reportBundledSkillTelemetry('install', 'npm_install', source);
  return payloads;
}
for (const source of [undefined, '', '  ', ' workbuddy ', 'another-host']) {
  const payloads = telemetryFixture(source);
  assert.strictEqual(payloads.length, 2);
  for (const payload of payloads) {
    assert.strictEqual(payload.source, 'npm_install');
    assert.strictEqual(payload.host_platform, (source || '').trim() || undefined);
    assert.strictEqual(payload.platform, process.platform);
    assert.strictEqual(payload.event, 'install');
  }
}
assert.strictEqual(telemetryFixture('workbuddy', '1').length, 0);
console.log('Install source forwarding, precedence, omission, retry and telemetry payload checks passed');

// Exercise the actual install entry through JSON serialization; no real installation/network.
for (const [args, env, want] of [
  [[], { CODEX_THREAD_ID: 'private-id' }, 'codex'],
  [[], { CODEX_SESSION_ID: 'private-id', CODEX_THREAD_ID: 'another-id' }, 'codex'],
  [[], { CLAUDECODE: '1' }, 'claude_code'],
  [[], { CURSOR_AGENT: '1' }, 'cursor'],
  [[], { GEMINI_CLI: 'true' }, 'gemini_cli'],
  [[], { PIPPIT_CLI_SOURCE: ' workbuddy ', CODEX_THREAD_ID: 'private-id' }, 'workbuddy'],
  [['--source= doubao_office '], { PIPPIT_CLI_SOURCE: 'workbuddy', CODEX_THREAD_ID: 'private-id' }, 'doubao_office'],
  [['--source', ''], { PIPPIT_CLI_SOURCE: 'workbuddy', CODEX_THREAD_ID: 'private-id' }, undefined],
  [['--source=  '], { CODEX_THREAD_ID: 'private-id' }, undefined],
  [[], { PIPPIT_CLI_SOURCE: '  ', CODEX_THREAD_ID: 'private-id' }, 'codex'],
  [[], { CODEX_THREAD_ID: 'private-id', CLAUDECODE: '1' }, undefined],
  [[], { CLAUDECODE: '0', CURSOR_AGENT: ' FALSE ', GEMINI_CLI: '' }, undefined],
  [[], {}, undefined],
]) {
  const { payloads } = installFixture(args, undefined, false, env);
  assert.strictEqual(payloads.length, 2);
  for (const payload of payloads) {
    assert.strictEqual(payload.host_platform, want);
    assert.strictEqual(payload.source, 'npx_install');
    assert.strictEqual(payload.event, 'install');
    assert.strictEqual(payload.platform, process.platform);
    assert(!JSON.stringify(payload).includes('private-id'));
  }
}
console.log('Install runtime attribution and serialized payload regression checks passed');

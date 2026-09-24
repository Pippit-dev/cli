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
function installFixture(args, source, retry = false) {
  const reports = [], installs = [];
  const proc = { argv: [], env: source === undefined ? {} : { PIPPIT_CLI_SOURCE: source } };
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
    './telemetry': { reportBundledSkillTelemetry: (...args) => reports.push(args) },
  };
  vm.runInNewContext(fs.readFileSync(require.resolve('./install-wizard'), 'utf8'), {
    module, process: proc, console: { log() {}, error() {} },
    require: name => modules[name] || require(name),
  });
  module.exports.main(args);
  return { reports, installs, proc };
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
function telemetryFixture(source, disabled) {
  const payloads = [];
  const request = () => ({ on() {}, end: body => payloads.push(JSON.parse(body)) });
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(require.resolve('./telemetry'), 'utf8'), {
    module, URL, Buffer, console,
    process: { env: { PIPPIT_CLI_DISABLE_TELEMETRY: disabled } },
    require: name => ['http', 'https'].includes(name) ? { request } : require(name),
  });
  module.exports.reportBundledSkillTelemetry('install', 'npm_install', source);
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

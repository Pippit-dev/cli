#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { isWindows, run, runSilent } = require('./platform');
const { DEFAULT_PKG, installGlobalPackageSkills } = require('./skills');
const { reportBundledSkillTelemetry } = require('./telemetry');

const VERSION = require('../package.json').version.replace(/-.*$/, '');

const INSTALL_HELP = `Install or update the global Pippit CLI and its bundled skills.

Usage:
  pippit-tool-cli install
  pippit-tool-cli install --help
  npx @pippit-dev/cli install

Requires npm, npx, network access, and permission to write the global npm package
and skill directories. Installs the version of this package by default.
PIPPIT_CLI_INSTALL_PACKAGE can override the package/version to install.

Options:
  --source HOST  Real host identifier reported as host_platform; unknown hosts omit it
                 Defaults to PIPPIT_CLI_SOURCE, then known host runtime markers; an explicit empty value omits host_platform
  -h, --help  Show this help without installing, updating, or sending telemetry
`;

function defaultInstallPackage() {
  return `${DEFAULT_PKG}@${VERSION}`;
}

function installPackage() {
  return process.env.PIPPIT_CLI_INSTALL_PACKAGE || defaultInstallPackage();
}

function getGloballyInstalledVersion() {
  try {
    const out = runSilent('npm', ['list', '-g', DEFAULT_PKG], { timeout: 15000 });
    const match = out.toString().match(/@(\d+\.\d+\.\d+[^\s]*)/);
    return match ? match[1] : 'unknown';
  } catch (_) {
    return null;
  }
}

function whichPippitToolCli() {
  try {
    const prefix = runSilent('npm', ['prefix', '-g'], { timeout: 15000 }).toString().trim();
    const bin = isWindows
      ? path.join(prefix, 'pippit-tool-cli.cmd')
      : path.join(prefix, 'bin', 'pippit-tool-cli');
    if (fs.existsSync(bin)) return bin;
  } catch (_) {
    // Fall back to PATH lookup.
  }

  try {
    const cmd = isWindows ? 'where' : 'which';
    return runSilent(cmd, ['pippit-tool-cli']).toString().split('\n')[0].trim();
  } catch (_) {
    return null;
  }
}

function main(args = process.argv.slice(2)) {
  if (args.includes('--help') || args.includes('-h')) {
    console.log(INSTALL_HELP);
    return;
  }
  let source;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--source' && i + 1 < args.length && !args[i + 1].startsWith('--')) {
      source = args[++i];
    } else if (args[i].startsWith('--source=')) {
      source = args[i].slice('--source='.length);
    } else {
      console.error(`Invalid install argument or missing value: ${args[i]}. Run pippit-tool-cli install --help.`);
      process.exitCode = 1;
      return;
    }
  }
  if (source !== undefined) source = source.trim();
  const pkg = installPackage();
  const installed = getGloballyInstalledVersion();
  if (installed) {
    console.log(`Updating global pippit-tool-cli (${installed}) via ${pkg}...`);
  } else {
    console.log(`Installing ${pkg} globally...`);
  }
  run('npm', ['install', '-g', pkg], {
    timeout: 120000,
    env: { ...process.env, PIPPIT_CLI_SKIP_SKILLS: '1' },
  });

  console.log('Installing pippit-tool-cli skills...');
  try {
    installGlobalPackageSkills(DEFAULT_PKG);
  } catch (err) {
    if (!installed) {
      throw err;
    }
    console.log('Existing global package does not contain skills; reinstalling...');
    run('npm', ['install', '-g', pkg], { timeout: 120000, env: { ...process.env, PIPPIT_CLI_SKIP_SKILLS: '1' } });
    installGlobalPackageSkills(DEFAULT_PKG);
  }

  const bin = whichPippitToolCli();
  if (!bin) {
    console.error('pippit-tool-cli was installed, but no global command was found in npm prefix.');
    console.error("Check that npm's global bin directory is in PATH.");
    process.exit(1);
  }

  console.log(`pippit-tool-cli is ready: ${bin}`);
  reportBundledSkillTelemetry('install', 'npx_install', source);
  console.log('Try: pippit-tool-cli short-drama +submit-run --message "写一个短剧开头"');
}

if (require.main === module) {
  main();
}

module.exports = {
  defaultInstallPackage,
  installPackage,
  main,
};

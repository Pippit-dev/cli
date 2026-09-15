#!/usr/bin/env node

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { isCanvasCommand, runCanvasCommand } = require('./canvas-command');
const { maybeWarnNewVersion } = require('./version-check');

const ext = process.platform === 'win32' ? '.exe' : '';
const bin = path.join(__dirname, '..', 'bin', 'pippit-tool-cli' + ext);
const args = process.argv.slice(2);

const oldBin = bin + '.old';
function restoreOldBinary() {
  try {
    if (fs.existsSync(bin)) {
      fs.rmSync(bin, { force: true });
    }
    fs.renameSync(oldBin, bin);
    return true;
  } catch (_) {
    return false;
  }
}

function runCanvas() {
  runCanvasCommand(args, {
    nativeInvocation: { command: bin },
  })
    .then((exitCode) => {
      if (exitCode) process.exitCode = exitCode;
    })
    .catch((error) => {
      console.error(error && error.message ? error.message : String(error));
      process.exitCode = 1;
    });
}

function main() {
  // Installation help must work before any native binary preparation.
  if (args[0] === 'install') {
    return require('./install-wizard.js').main(args.slice(1));
  }

  const helpRequested = args.includes('--help') || args.includes('-h') || args[0] === 'help';
  if (isCanvasCommand(args) && (args[2] !== 'run' || helpRequested)) {
    return runCanvas();
  }

  if (!helpRequested && args.length > 0 && process.platform === 'win32' && fs.existsSync(oldBin)) {
    if (!fs.existsSync(bin)) {
      restoreOldBinary();
    } else {
      try {
        execFileSync(bin, ['--help'], { stdio: 'ignore', timeout: 10000 });
        fs.rmSync(oldBin, { force: true });
      } catch (_) {
        restoreOldBinary();
      }
    }
  }

  if (!helpRequested && args.length > 0) maybeWarnNewVersion(args);

  if (!fs.existsSync(bin)) {
    if (helpRequested || args.length === 0) {
      console.error(
        'Native CLI binary is missing. Reinstall @pippit-dev/cli to restore native command help; canvas command --help and install --help remain available.',
      );
      process.exitCode = 1;
      return;
    }
    try {
      execFileSync(process.execPath, [path.join(__dirname, 'install.js')], {
        stdio: 'inherit',
        env: { ...process.env, PIPPIT_CLI_RUN: 'true' },
      });
    } catch (_) {
      console.error(
        '\nFailed to prepare pippit-tool-cli binary.\n' +
          'Check your network connection and reinstall the npm package to download the prebuilt binary.\n',
      );
      process.exit(1);
    }
  }

  if (isCanvasCommand(args)) {
    runCanvas();
  } else {
    try {
      execFileSync(bin, args, { stdio: 'inherit' });
    } catch (e) {
      process.exit(e.status || 1);
    }
  }
}

main();

const { spawnSync } = require('child_process');

function traeCaller(binary) {
  try {
    const result = spawnSync(binary(), ['__trae_caller'], {
      encoding: 'utf8', timeout: 300, maxBuffer: 1024, windowsHide: true,
      stdio: ['ignore', 'pipe', 'ignore'],
    });
    return result.status === 0 && result.stdout.trim() === 'trae';
  } catch {
    return false;
  }
}

module.exports = { traeCaller };

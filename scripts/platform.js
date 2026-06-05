const { execFileSync } = require("child_process");

const isWindows = process.platform === "win32";

function execCmd(cmd, args, opts = {}) {
  if (isWindows) {
    return execFileSync("cmd.exe", ["/c", cmd, ...args], opts);
  }
  return execFileSync(cmd, args, opts);
}

function run(cmd, args, opts = {}) {
  return execCmd(cmd, args, { stdio: "inherit", ...opts });
}

function runSilent(cmd, args, opts = {}) {
  return execCmd(cmd, args, {
    stdio: ["ignore", "pipe", "pipe"],
    ...opts,
  });
}

module.exports = {
  execCmd,
  isWindows,
  run,
  runSilent,
};

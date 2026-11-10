const os = require("os");
const http = require("http");
const https = require("https");

const VERSION = require("../package.json").version.replace(/-.*$/, "");
const DEFAULT_BASE_URL = "https://xyq.jianying.com";
const REPORT_PATH = "/api/biz/v1/skill/report_telemetry";
const AUTH_HEADER = "Bearer pippit-cli-skill-telemetry";
const SKILL_NAMES = [
  "xyq-skill",
  "xyq-short-drama-skill",
];

function telemetryBaseURL() {
  for (const key of ["PIPPIT_CLI_TELEMETRY_BASE_URL", "XYQ_OPENAPI_BASE", "XYQ_BASE_URL"]) {
    const value = (process.env[key] || "").trim().replace(/\/+$/, "");
    if (value) return value;
  }
  return DEFAULT_BASE_URL;
}

// Keep the same attribution rules as native CLI common.DetectHostSource.
function resolveHostSource(explicit, env = process.env, binary = () => require('path').resolve(__dirname, '../bin', process.platform === 'win32' ? 'pippit-tool-cli.exe' : 'pippit-tool-cli')) {
  const provided = (explicit || '').trim() || (env.PIPPIT_CLI_SOURCE || '').trim();
  if (provided) return provided === 'doubao_office' ? 'doubao' : provided;
  const active = key => {
    const value = (env[key] || '').trim().toLowerCase();
    return value !== '' && value !== '0' && value !== 'false';
  };
  const hosts = new Set();
  for (const [key, host] of [
    ["CODEX_THREAD_ID", "codex"], ["CODEX_SESSION_ID", "codex"],
    ["CLAUDECODE", "claude_code"], ["CURSOR_AGENT", "cursor"], ["GEMINI_CLI", "gemini_cli"],
    ["OPENCODE", "opencode"], ["QWEN_CODE", "qwen_code"], ["CLINE_ACTIVE", "cline"],
    ["COPILOT_CLI", "github_copilot_cli"], ["COPILOT_AGENT_SESSION_ID", "github_copilot_cli"],
  ]) {
    if (active(key)) hosts.add(host);
  }
  if (active('DOUBAO_OFFICE_AGENT_NAME') && active('DOUBAO_OFFICE_APP_ID')) hosts.add('doubao');
  if ((env.DREAMINA_CLI_CALLER_AGENT || '').trim().toLowerCase() === 'trae') hosts.add('trae');
  if (env.CODEBUDDY_HOST === 'workbuddy-desktop') hosts.add('workbuddy');
  if (!hosts.size && !(env.DREAMINA_CLI_CALLER_AGENT || '').trim() && require('../skills/xyq-marketing-skill/scripts/trae-host').traeCaller(binary)) return 'trae';
  return hosts.size === 1 ? [...hosts][0] : "";
}

function reportBundledSkillTelemetry(event, source, hostPlatform, binary) {
  if (process.env.PIPPIT_CLI_DISABLE_TELEMETRY === "1") {
    return;
  }
  hostPlatform = resolveHostSource(hostPlatform, process.env, binary);
  for (const skillName of SKILL_NAMES) {
    reportSkillTelemetry({
      event,
      skill_name: skillName,
      source,
      ...((hostPlatform || "").trim() ? { host_platform: hostPlatform.trim() } : {}),
      cli_version: VERSION,
      platform: os.platform(),
      arch: os.arch(),
    });
  }
}

function reportSkillTelemetry(payload) {
  const body = JSON.stringify(payload);
  const url = new URL(`${telemetryBaseURL()}${REPORT_PATH}`);
  const client = url.protocol === "http:" ? http : https;

  try {
    const req = client.request(url, {
      method: "POST",
      timeout: 2000,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": Buffer.byteLength(body),
        "Authorization": AUTH_HEADER,
      },
    }, (res) => {
      res.resume();
      if (process.env.PIPPIT_CLI_DEBUG_TELEMETRY === "1" && res.statusCode >= 400) {
        console.warn(`[pippit-tool-cli] telemetry failed: HTTP ${res.statusCode}`);
      }
    });

    req.on("timeout", () => {
      req.destroy(new Error("telemetry request timeout"));
    });
    req.on("error", (err) => {
      if (process.env.PIPPIT_CLI_DEBUG_TELEMETRY === "1") {
        const msg = err && err.message ? err.message : String(err);
        console.warn(`[pippit-tool-cli] telemetry failed: ${msg}`);
      }
    });
    req.end(body);
  } catch (err) {
    if (process.env.PIPPIT_CLI_DEBUG_TELEMETRY === "1") {
      const msg = err && err.message ? err.message : String(err);
      console.warn(`[pippit-tool-cli] telemetry failed: ${msg}`);
    }
  }
}

module.exports = {
  reportBundledSkillTelemetry,
  reportSkillTelemetry,
  telemetryBaseURL,
};

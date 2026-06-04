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

function reportBundledSkillTelemetry(event, source) {
  if (process.env.PIPPIT_CLI_DISABLE_TELEMETRY === "1") {
    return;
  }
  for (const skillName of SKILL_NAMES) {
    reportSkillTelemetry({
      event,
      skill_name: skillName,
      source,
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
        "x-use-ppe": "1",
        "x-tt-env": "ppe_harness_novel_v2",
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

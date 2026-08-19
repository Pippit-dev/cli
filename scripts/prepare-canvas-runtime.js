#!/usr/bin/env node

"use strict";

const crypto = require("crypto");
const fs = require("fs");
const { builtinModules } = require("module");
const path = require("path");
const { spawnSync } = require("child_process");

const ARTIFACT_NAME = "xyq-canvas-command-runtime.cjs";
const CHECKSUM_NAME = `${ARTIFACT_NAME}.sha256`;
const LEGAL_NAME = `${ARTIFACT_NAME}.LEGAL.txt`;
const REQUIRED_EXPORTS = [
  "createPippitAssetRuntime",
  "createPippitAssetSdkCanvasLoader",
  "createPippitAssetServiceTransport",
  "createXyqCanvasCommandRuntime",
  "createXyqCanvasOpencodeToolDefinitions",
];
const BUILTIN_MODULES = new Set([
  ...builtinModules,
  ...builtinModules.map((name) => `node:${name}`),
]);

function readExpectedChecksums(checksumPath) {
  let value;
  try {
    value = fs.readFileSync(checksumPath, "utf8").trim();
  } catch (error) {
    if (error && error.code === "ENOENT") {
      throw new Error(`缺少固定画布运行时校验文件：${checksumPath}`);
    }
    throw error;
  }
  const lines = value.split(/\r?\n/);
  const expectedNames = new Set([ARTIFACT_NAME, LEGAL_NAME]);
  const checksums = new Map();
  for (const line of lines) {
    const match = line.match(/^([a-fA-F0-9]{64})\s+\*?([^\s]+)$/);
    if (!match || !expectedNames.has(match[2]) || checksums.has(match[2])) {
      throw new Error(`画布运行时校验文件格式无效：${checksumPath}`);
    }
    checksums.set(match[2], match[1].toLowerCase());
  }
  if (checksums.size !== expectedNames.size) {
    throw new Error(`画布运行时校验文件格式无效：${checksumPath}`);
  }
  return checksums;
}

function hashFile(filePath) {
  return crypto.createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
}

function assertRegularFile(filePath) {
  let stat;
  try {
    stat = fs.lstatSync(filePath);
  } catch (error) {
    if (error && error.code === "ENOENT") {
      throw new Error(`缺少固定画布运行时产物：${filePath}`);
    }
    throw error;
  }
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`画布运行时产物必须是普通文件：${filePath}`);
  }
  if (stat.size === 0) throw new Error(`画布运行时产物为空：${filePath}`);
}

function assertRuntimeExports(filePath) {
  const verification = [
    "const artifact = require(process.argv[1]);",
    `const required = ${JSON.stringify(REQUIRED_EXPORTS)};`,
    "for (const name of required) {",
    "  if (typeof artifact[name] !== 'function') {",
    "    throw new Error(`missing export: ${name}`);",
    "  }",
    "}",
    "if (!Array.isArray(artifact.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS)) {",
    "  throw new Error('missing mutation command catalog');",
    "}",
    "if (!Array.isArray(artifact.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS)) {",
    "  throw new Error('missing registered command catalog');",
    "}",
  ].join("\n");
  const result = spawnSync(process.execPath, ["-e", verification, filePath], {
    encoding: "utf8",
    timeout: 30_000,
    windowsHide: true,
  });
  if (result.error) throw new Error(`无法校验画布运行时产物：${result.error.message}`);
  if (result.status !== 0) {
    const detail = (result.stderr || result.stdout || "unknown error").trim();
    throw new Error(`画布运行时导出校验失败：${detail}`);
  }
}

function assertSelfContained(filePath) {
  const source = fs.readFileSync(filePath, "utf8");
  const requires = source.matchAll(/(?:\brequire|\b__require)\(\s*["']([^"']+)["']\s*\)/g);
  for (const match of requires) {
    if (!BUILTIN_MODULES.has(match[1])) {
      throw new Error(`画布运行时仍依赖外部模块：${match[1]}`);
    }
  }
  if (/sourceMappingURL\s*=/.test(source)) {
    throw new Error("画布运行时产物不能携带源码映射引用");
  }
}

function stageCanvasRuntime(options = {}) {
  const root = options.root || path.join(__dirname, "..");
  const sourceDirectory = options.sourceDirectory || path.join(root, "release-artifacts", "canvas-runtime");
  const destinationDirectory = options.destinationDirectory || path.join(root, "dist");
  const sourcePath = path.join(sourceDirectory, ARTIFACT_NAME);
  const legalSourcePath = path.join(sourceDirectory, LEGAL_NAME);
  const checksumPath = path.join(sourceDirectory, CHECKSUM_NAME);
  const destinationPath = path.join(destinationDirectory, ARTIFACT_NAME);
  const legalDestinationPath = path.join(destinationDirectory, LEGAL_NAME);
  const checksumDestinationPath = path.join(destinationDirectory, CHECKSUM_NAME);

  assertRegularFile(sourcePath);
  assertRegularFile(legalSourcePath);
  const expectedChecksums = readExpectedChecksums(checksumPath);
  const actualChecksum = hashFile(sourcePath);
  const actualLegalChecksum = hashFile(legalSourcePath);
  if (actualChecksum !== expectedChecksums.get(ARTIFACT_NAME)) {
    throw new Error(
      `画布运行时 SHA-256 不匹配：期望 ${expectedChecksums.get(ARTIFACT_NAME)}，实际 ${actualChecksum}`
    );
  }
  if (actualLegalChecksum !== expectedChecksums.get(LEGAL_NAME)) {
    throw new Error(
      `画布运行时 LEGAL SHA-256 不匹配：期望 ${expectedChecksums.get(LEGAL_NAME)}，实际 ${actualLegalChecksum}`
    );
  }
  assertSelfContained(sourcePath);
  assertRuntimeExports(sourcePath);

  if (!options.checkOnly) {
    fs.mkdirSync(destinationDirectory, { recursive: true });
    const temporaryPath = `${destinationPath}.${process.pid}.tmp`;
    const legalTemporaryPath = `${legalDestinationPath}.${process.pid}.tmp`;
    const checksumTemporaryPath = `${checksumDestinationPath}.${process.pid}.tmp`;
    try {
      fs.copyFileSync(sourcePath, temporaryPath, fs.constants.COPYFILE_EXCL);
      fs.copyFileSync(legalSourcePath, legalTemporaryPath, fs.constants.COPYFILE_EXCL);
      fs.copyFileSync(checksumPath, checksumTemporaryPath, fs.constants.COPYFILE_EXCL);
      fs.chmodSync(temporaryPath, 0o644);
      fs.chmodSync(legalTemporaryPath, 0o644);
      fs.chmodSync(checksumTemporaryPath, 0o644);
      fs.rmSync(destinationPath, { force: true });
      fs.rmSync(legalDestinationPath, { force: true });
      fs.rmSync(checksumDestinationPath, { force: true });
      fs.renameSync(temporaryPath, destinationPath);
      fs.renameSync(legalTemporaryPath, legalDestinationPath);
      fs.renameSync(checksumTemporaryPath, checksumDestinationPath);
    } finally {
      fs.rmSync(temporaryPath, { force: true });
      fs.rmSync(legalTemporaryPath, { force: true });
      fs.rmSync(checksumTemporaryPath, { force: true });
    }
    if (hashFile(destinationPath) !== expectedChecksums.get(ARTIFACT_NAME)) {
      throw new Error(`画布运行时装配后校验失败：${destinationPath}`);
    }
    if (hashFile(legalDestinationPath) !== expectedChecksums.get(LEGAL_NAME)) {
      throw new Error(`画布运行时 LEGAL 装配后校验失败：${legalDestinationPath}`);
    }
    if (fs.readFileSync(checksumDestinationPath, "utf8") !== fs.readFileSync(checksumPath, "utf8")) {
      throw new Error(`画布运行时 checksum 装配后校验失败：${checksumDestinationPath}`);
    }
  }

  return {
    actualChecksum,
    actualLegalChecksum,
    checksumDestinationPath,
    destinationPath,
    legalDestinationPath,
    legalSourcePath,
    sourcePath,
  };
}

function main() {
  const unknownArgs = process.argv.slice(2).filter((value) => value !== "--check-only");
  if (unknownArgs.length) throw new Error(`未知参数：${unknownArgs.join(" ")}`);
  const checkOnly = process.argv.includes("--check-only");
  const result = stageCanvasRuntime({ checkOnly });
  console.error(
    checkOnly
      ? `画布运行时产物校验通过：${result.actualChecksum}`
      : `画布运行时产物已装配：${result.destinationPath}`
  );
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(error && error.message ? error.message : String(error));
    process.exitCode = 1;
  }
}

module.exports = {
  ARTIFACT_NAME,
  CHECKSUM_NAME,
  LEGAL_NAME,
  assertSelfContained,
  hashFile,
  readExpectedChecksums,
  stageCanvasRuntime,
};

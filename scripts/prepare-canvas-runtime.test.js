#!/usr/bin/env node

"use strict";

const assert = require("assert");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");
const {
  ARTIFACT_NAME,
  CHECKSUM_NAME,
  LEGAL_NAME,
  stageCanvasRuntime,
} = require("./prepare-canvas-runtime");
const { runCanvasCommand } = require("./canvas-command");

const VALID_RUNTIME = `
module.exports = {
  createPippitAssetRuntime() {},
  createPippitAssetSdkCanvasLoader() {},
  createPippitAssetServiceTransport() {},
  createXyqCanvasCommandRuntime() {},
  createXyqCanvasOpencodeToolDefinitions() {},
  XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS: [],
  XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS: [],
};
`;
const VALID_LEGAL = "Third-party notices for the fixed canvas command runtime.\n";

function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex");
}

function createFixture() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pippit-canvas-runtime-test-"));
  const sourceDirectory = path.join(root, "release-artifacts", "canvas-runtime");
  fs.mkdirSync(sourceDirectory, { recursive: true });
  return { root, sourceDirectory };
}

function writeFixedRuntime(sourceDirectory, contents = VALID_RUNTIME) {
  fs.writeFileSync(path.join(sourceDirectory, ARTIFACT_NAME), contents);
  fs.writeFileSync(path.join(sourceDirectory, LEGAL_NAME), VALID_LEGAL);
  fs.writeFileSync(
    path.join(sourceDirectory, CHECKSUM_NAME),
    `${sha256(contents)}  ${ARTIFACT_NAME}\n${sha256(VALID_LEGAL)}  ${LEGAL_NAME}\n`
  );
}

async function run() {
  const repositoryRoot = path.join(__dirname, "..");
  const packageJSON = require(path.join(repositoryRoot, "package.json"));
  const gitAttributes = fs.readFileSync(path.join(repositoryRoot, ".gitattributes"), "utf8");
  const releaseWorkflow = fs.readFileSync(
    path.join(repositoryRoot, ".github", "workflows", "release.yml"),
    "utf8"
  );
  const releaseCleanIndex = releaseWorkflow.indexOf("args: release --clean");
  const stageRuntimeIndex = releaseWorkflow.indexOf("npm run prepare:canvas-runtime");
  const verifyPackageIndex = releaseWorkflow.indexOf("npm pack --dry-run");
  const publishIndex = releaseWorkflow.indexOf("npm publish --provenance --access public");
  assert.strictEqual(packageJSON.scripts.prepack, "node scripts/prepare-canvas-runtime.js");
  assert(packageJSON.files.includes(`dist/${ARTIFACT_NAME}`));
  assert(packageJSON.files.includes(`dist/${LEGAL_NAME}`));
  assert(packageJSON.files.includes(`dist/${CHECKSUM_NAME}`));
  assert(gitAttributes.includes(`release-artifacts/canvas-runtime/${ARTIFACT_NAME} -text`));
  assert(gitAttributes.includes(`release-artifacts/canvas-runtime/${LEGAL_NAME} -text`));
  assert(releaseCleanIndex !== -1 && releaseCleanIndex < stageRuntimeIndex);
  assert(stageRuntimeIndex < verifyPackageIndex);
  assert(verifyPackageIndex < publishIndex);

  const successful = createFixture();
  const externalDependency = createFixture();
  const missingLegal = createFixture();
  const missing = createFixture();
  const mismatched = createFixture();
  const singleChecksum = createFixture();
  const tamperedLegal = createFixture();
  try {
    writeFixedRuntime(successful.sourceDirectory);
    const staged = stageCanvasRuntime({ root: successful.root });
    assert.strictEqual(fs.readFileSync(staged.destinationPath, "utf8"), VALID_RUNTIME);
    assert.strictEqual(fs.readFileSync(staged.legalDestinationPath, "utf8"), VALID_LEGAL);
    assert.strictEqual(
      fs.readFileSync(staged.checksumDestinationPath, "utf8"),
      fs.readFileSync(path.join(successful.sourceDirectory, CHECKSUM_NAME), "utf8")
    );
    assert.strictEqual(staged.actualChecksum, sha256(VALID_RUNTIME));
    assert.strictEqual(staged.actualLegalChecksum, sha256(VALID_LEGAL));

    fs.rmSync(staged.destinationPath, { force: true });
    fs.rmSync(staged.legalDestinationPath, { force: true });
    fs.rmSync(staged.checksumDestinationPath, { force: true });
    stageCanvasRuntime({ checkOnly: true, root: successful.root });
    assert.strictEqual(fs.existsSync(staged.destinationPath), false);
    assert.strictEqual(fs.existsSync(staged.legalDestinationPath), false);
    assert.strictEqual(fs.existsSync(staged.checksumDestinationPath), false);

    assert.throws(
      () => stageCanvasRuntime({ root: missing.root }),
      /缺少固定画布运行时产物/
    );

    writeFixedRuntime(missingLegal.sourceDirectory);
    fs.rmSync(path.join(missingLegal.sourceDirectory, LEGAL_NAME));
    assert.throws(
      () => stageCanvasRuntime({ root: missingLegal.root }),
      /缺少固定画布运行时产物/
    );

    writeFixedRuntime(
      externalDependency.sourceDirectory,
      `require("private-runtime");\n${VALID_RUNTIME}`
    );
    assert.throws(
      () => stageCanvasRuntime({ root: externalDependency.root }),
      /仍依赖外部模块/
    );

    writeFixedRuntime(mismatched.sourceDirectory);
    fs.appendFileSync(path.join(mismatched.sourceDirectory, ARTIFACT_NAME), "// changed\n");
    assert.throws(
      () => stageCanvasRuntime({ root: mismatched.root }),
      /SHA-256 不匹配/
    );

    writeFixedRuntime(singleChecksum.sourceDirectory);
    fs.writeFileSync(
      path.join(singleChecksum.sourceDirectory, CHECKSUM_NAME),
      `${sha256(VALID_RUNTIME)}  ${ARTIFACT_NAME}\n`
    );
    assert.throws(
      () => stageCanvasRuntime({ root: singleChecksum.root }),
      /校验文件格式无效/
    );

    writeFixedRuntime(tamperedLegal.sourceDirectory);
    fs.appendFileSync(path.join(tamperedLegal.sourceDirectory, LEGAL_NAME), "changed\n");
    assert.throws(
      () => stageCanvasRuntime({ root: tamperedLegal.root }),
      /LEGAL SHA-256 不匹配/
    );
  } finally {
    fs.rmSync(successful.root, { force: true, recursive: true });
    fs.rmSync(externalDependency.root, { force: true, recursive: true });
    fs.rmSync(missingLegal.root, { force: true, recursive: true });
    fs.rmSync(missing.root, { force: true, recursive: true });
    fs.rmSync(mismatched.root, { force: true, recursive: true });
    fs.rmSync(singleChecksum.root, { force: true, recursive: true });
    fs.rmSync(tamperedLegal.root, { force: true, recursive: true });
  }

  stageCanvasRuntime({ root: repositoryRoot });
  let listOutput = "";
  await runCanvasCommand(["canvas", "command", "list"], {
    stdout: { write: (chunk) => { listOutput += String(chunk); } },
  });
  const catalog = JSON.parse(listOutput).commands;
  assert.strictEqual(catalog.length, 42);
  assert(catalog.some(({ name }) => name === "create_biz_node"));
  assert(!catalog.some(({ name }) => name === "invoke_command"));

  let describeOutput = "";
  await runCanvasCommand(["canvas", "command", "describe", "create_biz_node"], {
    stdout: { write: (chunk) => { describeOutput += String(chunk); } },
  });
  assert.strictEqual(JSON.parse(describeOutput).name, "create_biz_node");
  console.log("prepare-canvas-runtime tests passed");
}

run().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

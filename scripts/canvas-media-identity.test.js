'use strict';

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { ensureNodeRuntimeGlobals, runCanvasCommand } = require('./canvas-command');

// Real packaged SDK + CLI host. The native service process is replaced, so
// no account data is written and actual sync payloads remain inspectable.
async function main() {
  ensureNodeRuntimeGlobals();
  const sdk = require(
    path.resolve(process.argv[2] || path.join(__dirname, '../dist/xyq-canvas-command-runtime.cjs')),
  );
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'canvas-media-identity-'));
  const native = path.join(temp, 'native.cjs');
  const rootId = 'canvas-test';
  const pippitAssetId = '10155947700492';
  const assetId = '7693046454219440665';
  const nodeId = '10901596019212';
  const log = path.join(temp, 'writes.jsonl');
  const standalone = sdk.createXyqCanvasCommandRuntime({
    canvasId: rootId,
    transportFactory() {
      throw new Error('Fixture must stay offline');
    },
  });
  const rootAsset = structuredClone(standalone.store.getState().assets[rootId]);
  rootAsset.content.settings.autoLayout = {
    ...rootAsset.content.settings.autoLayout,
    strategy: 'dag-layered',
  };
  standalone.canvas.dispose();
  fs.writeFileSync(
    native,
    `
    const fs = require("fs");
    const args = process.argv.slice(2);
    const rootId = ${JSON.stringify(rootId)};
    const pippitAssetId = ${JSON.stringify(pippitAssetId)};
    const assetId = ${JSON.stringify(assetId)};
    const nodeId = ${JSON.stringify(nodeId)};
    const rootAsset = ${JSON.stringify(rootAsset)};
    if (args[0] === "status") {
      console.log(JSON.stringify({logged_in: true}));
    } else if (args[1] === "allocate") {
      console.log(JSON.stringify({asset_ids: [nodeId]}));
    } else if (args[1] === "get") {
      const ids = args.filter((_, i) => args[i-1] === "--asset-id");
      const assets = ids.map(id => id === pippitAssetId
        ? {PippitAssetID: id, AssetType: 1, SourceID: "source-id", Image: {asset_id: assetId}, version: 1}
        : id === rootId
          ? {PippitAssetID: id, AssetType: 4, TextInfo: {content: JSON.stringify(rootAsset)}, version: 1}
          : null).filter(Boolean);
      console.log(JSON.stringify({assets}));
    } else if (args[1] === "apply") {
      const request = JSON.parse(fs.readFileSync(0, "utf8"));
      fs.appendFileSync(${JSON.stringify(log)}, JSON.stringify(request) + "\\n");
      console.log(JSON.stringify({batch_id: request.batch_id, results: request.transactions.map(tx => ({
        transaction_id: tx.transaction_id, status: "ack",
        asset_versions: Object.fromEntries(tx.patches.map(p => [p.asset_id, 2]))
      }))}));
    } else {
      throw new Error("Unexpected command " + args.join(" "));
    }
  `,
  );
  async function create(initialData) {
    let output = '';
    const status = await runCanvasCommand(
      [
        'canvas',
        'command',
        'run',
        'create_biz_node',
        '--canvas-id',
        rootId,
        '--input',
        JSON.stringify({ nodeKind: 'image', id: nodeId, initialData }),
      ],
      {
        sdk,
        cwd: temp,
        nativeInvocation: { command: process.execPath, prefixArgs: [native] },
        stdout: {
          write(value) {
            output += value;
          },
        },
      },
    );
    return { status, output };
  }
  try {
    const result = await create({ pippitAssetId });
    assert.strictEqual(result.status, 0, result.output);
    assert.strictEqual(JSON.parse(result.output).ok, true);
    const writes = fs.readFileSync(log, 'utf8').trim().split('\n').map(JSON.parse);
    assert.strictEqual(writes.length, 1);
    const nodePatch = writes[0].transactions[0].patches.find((p) => p.asset_id === nodeId && p.path === '');
    assert(nodePatch, 'missing persisted media node');
    assert.strictEqual(nodePatch.value.pippitAssetId, nodeId);
    assert.strictEqual(nodePatch.value.content.pippitAssetId, pippitAssetId);
    assert.strictEqual(nodePatch.value.content.assetId, assetId);
    for (const wrongId of [pippitAssetId, nodeId, 'source-id', '7693046454219440666']) {
      const rejected = await create({ pippitAssetId, assetId: wrongId });
      assert.strictEqual(JSON.parse(rejected.output).ok, false);
      assert.match(rejected.output, /INVALID_MEDIA_IDENTITY/);
      assert.strictEqual(
        fs.readFileSync(log, 'utf8').trim().split('\n').length,
        1,
        'invalid command reached apply',
      );
    }
    console.log(
      'Canvas media identity runtime: Pippit-only input resolved; 4 wrong identities rejected before apply',
    );
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

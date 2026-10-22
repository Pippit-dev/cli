'use strict';

const assert = require('assert');
const path = require('path');
const packageRoot = path.resolve(process.argv[2] || path.join(__dirname, '..'));
const cli = require(path.join(packageRoot, 'scripts/canvas-command'));
cli.ensureNodeRuntimeGlobals();
const sdk = require(path.join(packageRoot, 'dist/xyq-canvas-command-runtime.cjs'));
const canvasId = '9000000000001';
const nodeId = '9000000000002';
const validRatio = { width: 16, height: 9 };
const privatePrompt = 'private ratio test prompt';
const permissions = [
  'canvas.read',
  'canvas.write',
  'canvas.patch',
  'canvas.asset.read',
  'canvas.asset.write',
];

// Exercise the real CLI, SDK, codec and flush. Only the native service boundary
// is replaced; invalid inputs must never reach the serialized apply request.
let active;
let checks = 0;
const native = cli.NativeCanvasClient.prototype;
const originals = new Map();
function replaceNative(name, method) {
  originals.set(name, native[name]);
  native[name] = method;
}

function applyPatch(patch) {
  if (patch.path === '') {
    if (patch.op === 'remove') delete active.assets[patch.asset_id];
    else active.assets[patch.asset_id] = structuredClone(patch.value);
    return;
  }
  const parts = patch.path
    .slice(1)
    .split('/')
    .map((part) => part.replace(/~1/g, '/').replace(/~0/g, '~'));
  let target = active.assets[patch.asset_id];
  for (const part of parts.slice(0, -1)) target = target[part];
  const key = parts.at(-1);
  if (patch.op === 'remove') delete target[key];
  else target[key] = structuredClone(patch.value);
}

async function seed(existing, ratio = validRatio) {
  const fixture = sdk.createXyqCanvasCommandRuntime({
    canvasId,
    transportFactory() {
      throw new Error('Fixture must stay offline');
    },
  });
  try {
    if (existing) {
      const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
        schema: cli.createSchemaFactory(),
        runtime: { store: fixture.store, permissions },
      });
      const result = JSON.parse(
        await tools.apply_mutations.execute({
          atomic: true,
          mutations: [{ kind: 'create_biz_node', nodeKind: 'video', id: nodeId }],
        }),
      );
      assert.strictEqual(result.ok, true);
    }
    active = { assets: structuredClone(fixture.store.getState().assets), saves: [] };
    if (existing) active.assets[nodeId].content.ratio = structuredClone(ratio);
  } finally {
    fixture.canvas.dispose();
  }
}

async function execute(command, input) {
  let output = '';
  const status = await cli.runCanvasCommand(
    [
      'canvas',
      'command',
      'run',
      command,
      '--canvas-id',
      canvasId,
      '--input',
      JSON.stringify(input),
    ],
    {
      sdk,
      stdout: {
        write(value) {
          output += value;
        },
      },
      stderr: { write() {} },
    },
  );
  checks++;
  return { status, result: JSON.parse(output), output };
}

async function main() {
  replaceNative('runJSON', async () => {
    throw new Error('Real native process forbidden');
  });
  replaceNative('ensureCanvasAccess', async () => ({ logged_in: true }));
  replaceNative('getAssets', async (ids) =>
    ids.flatMap((id) =>
      active.assets[id]
        ? [
            {
              PippitAssetID: id,
              TextInfo: { content: JSON.stringify(active.assets[id]) },
              version: 5,
            },
          ]
        : [],
    ),
  );
  replaceNative('getAssetsAllowMissing', native.getAssets);
  replaceNative('allocateAssetIds', async (count) =>
    Array.from({ length: count }, (_, i) => String(9000000000100 + i)),
  );
  replaceNative('apply', async (request) => {
    active.saves.push(structuredClone(request));
    for (const transaction of request.transactions)
      for (const patch of transaction.patches) applyPatch(patch);
    return {
      results: request.transactions.map((transaction) => ({
        transaction_id: transaction.transaction_id,
        status: 'ack',
        asset_versions: Object.fromEntries(transaction.patches.map((patch) => [patch.asset_id, 6])),
      })),
    };
  });
  const fetch = globalThis.fetch;
  globalThis.fetch = async () => {
    throw new Error('Live network forbidden');
  };
  try {
    const video = {
      pippitAssetId: nodeId,
      type: 'biz/video',
      content: { ratio: '16:9' },
      extra: {},
    };
    const invalid = [
      [
        'create_biz_node',
        {
          nodeKind: 'video',
          id: nodeId,
          initialData: { ratio: '16:9', generation: { prompt: privatePrompt } },
        },
        false,
      ],
      [
        'create_node',
        {
          node: { id: nodeId, type: 'biz/video', x: 0, y: 0, w: 480, h: 270, data: {} },
          assets: [video],
          autoCreateNodeAsset: false,
        },
        false,
      ],
      ['update_node_data', { id: nodeId, patch: { ratio: '16:9' } }, true],
      ['update_asset', { assetId: nodeId, contentPatch: { ratio: '16:9' } }, true],
      ...[
        { path: '', value: video },
        { path: '/content', value: { ratio: '16:9' } },
        { path: '/content/ratio', value: '16:9' },
        { path: '/content/ratio/width', value: '16' },
        { path: '/content/RATIO', value: '16:9' },
        { path: '/CONTENT', value: { ratio: '16:9' } },
      ].map((patch) => [
        'apply_patches',
        { patches: [{ assetId: nodeId, op: 'add', ...patch }] },
        true,
      ]),
      [
        'update_asset',
        { assetId: nodeId, contentPatch: { ratio: { Width: '16', height: 9 } } },
        true,
      ],
    ];
    for (const [command, input, existing] of invalid) {
      await seed(existing);
      const before = structuredClone(active.assets);
      const { status, result, output } = await execute(command, input);
      assert.strictEqual(status, 1, output);
      assert.strictEqual(result.ok, false, output);
      assert.match(output, /INVARIANT_FAILED/);
      assert.match(output, /content\/ratio/i);
      assert(!output.includes(privatePrompt), 'error exposed a private prompt');
      assert.strictEqual(active.saves.length, 0, `${command} sent an invalid save`);
      assert.deepStrictEqual(active.assets, before);
    }
    for (const ratio of [
      validRatio,
      { Width: 16, HEIGHT: 9 },
      {},
      { width: 0, height: -1 },
      null,
    ]) {
      await seed(true);
      const { status, output } = await execute('update_asset', {
        assetId: nodeId,
        contentPatch: { ratio },
      });
      assert.strictEqual(status, 0, output);
      assert.strictEqual(active.saves.length, 1);
      assert.deepStrictEqual(active.assets[nodeId].content.ratio, ratio);
    }
    await seed(false);
    const created = await execute('create_biz_node', {
      nodeKind: 'video',
      id: nodeId,
      defaultVideoRatio: '9:16',
    });
    assert.strictEqual(created.status, 0, created.output);
    assert.strictEqual(active.assets[nodeId].content.generation.ratio, '9:16');
    assert.deepStrictEqual(active.assets[nodeId].content.ratio, { width: 9, height: 16 });

    await seed(true, '16:9');
    const edited = await execute('update_asset', {
      assetId: nodeId,
      contentPatch: { title: 'Unrelated edit' },
    });
    assert.strictEqual(edited.status, 0, edited.output);
    assert.strictEqual(active.assets[nodeId].content.ratio, '16:9');
    const repaired = await execute('apply_patches', {
      patches: [{ assetId: nodeId, op: 'remove', path: '/content/ratio' }],
    });
    assert.strictEqual(repaired.status, 0, repaired.output);
    assert(!Object.hasOwn(active.assets[nodeId].content, 'ratio'));

    await seed(true);
    const atomic = await execute('apply_mutations', {
      atomic: true,
      mutations: [
        { kind: 'update_node_data', id: nodeId, patch: { title: 'Must roll back' } },
        { kind: 'update_node_data', id: nodeId, patch: { ratio: '16:9' } },
      ],
    });
    assert.strictEqual(atomic.status, 1, atomic.output);
    assert.strictEqual(active.saves.length, 0);
    assert.notStrictEqual(active.assets[nodeId].content.title, 'Must roll back');

    await seed(true);
    const forcedAtomic = await execute('apply_mutations', {
      atomic: false,
      mutations: [
        { kind: 'update_node_data', id: nodeId, patch: { title: 'Valid committed edit' } },
        { kind: 'update_node_data', id: nodeId, patch: { ratio: '16:9' } },
      ],
    });
    assert.strictEqual(forcedAtomic.status, 1, forcedAtomic.output);
    assert.strictEqual(active.saves.length, 0);
    assert.deepStrictEqual(active.assets[nodeId].content.ratio, validRatio);

    // The lower-level SDK still supports non-atomic callers, as before PR #38.
    const document = { rootCanvasId: canvasId, assets: structuredClone(active.assets) };
    const standalone = sdk.createXyqCanvasCommandRuntime({
      canvasId,
      transportFactory: ({ clientId }) => ({
        clientId,
        commit: async () => ({ status: 'ack' }),
        fetchSnapshot: async () => ({ document }),
        onInvalidate: () => () => {},
        onConnectionChange: () => () => {},
        getConnectionState: () => 'open',
        close() {},
      }),
    });
    try {
      standalone.canvas.start();
      await standalone.canvas.whenReady();
      const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
        schema: cli.createSchemaFactory(),
        runtime: { store: standalone.store, permissions },
      });
      const partial = JSON.parse(
        await tools.apply_mutations.execute({
          atomic: false,
          mutations: [
            {
              kind: 'update_asset',
              assetId: nodeId,
              contentPatch: { title: 'Valid committed edit' },
            },
            { kind: 'update_asset', assetId: nodeId, contentPatch: { ratio: '16:9' } },
          ],
        }),
      );
      assert.strictEqual(partial.ok, true);
      assert.deepStrictEqual(
        partial.data.results.map((result) => result.status),
        ['committed', 'aborted'],
      );
      assert.strictEqual(
        standalone.store.getState().assets[nodeId].content.title,
        'Valid committed edit',
      );
      assert.deepStrictEqual(standalone.store.getState().assets[nodeId].content.ratio, validRatio);
    } finally {
      standalone.canvas.dispose();
    }
    console.log(
      `Canvas video ratio: ${checks} packaged CLI cases passed; invalid writes never reached apply`,
    );
  } finally {
    globalThis.fetch = fetch;
    for (const [name, original] of originals) native[name] = original;
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

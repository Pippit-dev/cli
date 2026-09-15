'use strict';

const assert = require('assert');
const path = require('path');
const { deserialize, serialize } = require('v8');
const { createSchemaFactory, ensureNodeRuntimeGlobals } = require('./canvas-command');

// These tests execute the packaged runtime's public tool definitions, not source
// substitutes. Fixture contracts come from canvas-sdk/src/tests/opencode-tools,
// xyq-canvas-sdk/src/tests/{opencode-tools,role-appearance-command,
// role-source-infos-command}, and the packaged public input schemas.
ensureNodeRuntimeGlobals();
const sdk = require(
  path.resolve(process.argv[2] || path.join(__dirname, '../dist/xyq-canvas-command-runtime.cjs')),
);
const mutationNames = new Set(sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS.map(({ kind }) => kind));
const registered = sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS;
const excluded = new Set([
  'xyq.scene3d.apply',
  'xyq.scene3d.query',
  'xyq.timeline.apply',
  'xyq.timeline.query',
]);
const permissions = [
  'canvas.read',
  'canvas.write',
  'canvas.patch',
  'canvas.asset.read',
  'canvas.asset.write',
  'canvas.checkpoint.create',
  'canvas.checkpoint.restore',
  'canvas.permission.read',
];
const clone = (value) => deserialize(serialize(value));
const cases = [];
const records = [];
let networkCalls = 0;
globalThis.fetch = async () => {
  networkCalls += 1;
  throw new Error('Canvas operation tests must remain offline');
};

function test(command, scenario, execute) {
  cases.push({ command, scenario, execute });
}
function success(result) {
  assert.strictEqual(result.ok, true, JSON.stringify(result));
  for (const entry of result.data?.results || [])
    assert.notStrictEqual(entry.status, 'aborted', JSON.stringify(result));
  return result;
}
function failure(result, pattern) {
  const aborted = result.data?.results?.find(({ status }) => status === 'aborted');
  assert(result.ok === false || aborted, `Expected failure, got ${JSON.stringify(result)}`);
  if (pattern) assert.match(result.message || aborted.reason, pattern);
}

function fixture(index) {
  const standalone = sdk.createXyqCanvasCommandRuntime({
    canvasId: `canvas-operations-${index}`,
    transportFactory() {
      networkCalls += 1;
      throw new Error('No transport is allowed in document operation tests');
    },
  });
  const store = standalone.store;
  const runtime = { store, permissions, createCheckpointId: () => `checkpoint-${index}` };
  const definitions = sdk.createXyqCanvasOpencodeToolDefinitions({ schema: createSchemaFactory(), runtime });
  const root = () => store.getRootCanvasContent();
  const node = (id) => root().nodes[id];
  const asset = (id) => store.getState().assets[id];
  // The same public definitions and mutation/invoke dispatch as the CLI use;
  // no tested action calls store.commands directly.
  async function call(name, input = {}, defs = definitions) {
    if (defs[name]) return JSON.parse(await defs[name].execute(input));
    assert(mutationNames.has(name) || registered.some((entry) => entry.name === name), name);
    const mutation = mutationNames.has(name)
      ? { ...input, kind: name }
      : { kind: 'invoke_command', name, args: [input] };
    return JSON.parse(
      await defs.apply_mutations.execute({ atomic: true, intent: `execute ${name}`, mutations: [mutation] }),
    );
  }
  const write = async (name, input) => success(await call(name, input));
  async function biz(kind, id = kind, initialData) {
    await write('create_biz_node', { nodeKind: kind, id, ...(initialData ? { initialData } : {}) });
    return id;
  }
  async function nodes() {
    // Existing registered text node protocol, with content in its node asset.
    for (const [index, id] of ['a', 'b', 'c'].entries()) {
      await write('create_node', {
        node: {
          id,
          type: 'biz/text',
          x: [0, 250, 500][index],
          y: [0, 120, 300][index],
          w: [100, 50, 100][index],
          h: [80, 60, 100][index],
          parentId: null,
          order: index,
          data: {},
        },
        assets: [{ pippitAssetId: id, type: 'biz/text', content: { plainText: id }, extra: {} }],
        autoCreateNodeAsset: false,
      });
    }
    return ['a', 'b', 'c'];
  }
  async function edge(id = 'edge', source = 'a', target = 'b') {
    await write('create_edge', { edge: { id, type: 'reference', source, target, data: {} } });
    return id;
  }
  async function group(ids = ['a', 'b']) {
    const before = new Set(Object.keys(root().nodes));
    await write('group_nodes', { ids });
    const id = Object.keys(root().nodes).find((id) => !before.has(id));
    assert(id, 'group operation must create a node');
    return id;
  }
  function world(id) {
    const n = node(id);
    const parent = n.parentId ? world(n.parentId) : { x: 0, y: 0 };
    return { x: n.x + parent.x, y: n.y + parent.y };
  }
  return {
    standalone,
    store,
    runtime,
    definitions,
    root,
    node,
    asset,
    call,
    write,
    biz,
    nodes,
    edge,
    group,
    world,
  };
}

test('set_title', 'metadata', async (f) => {
  await f.write('set_title', { title: 'Canvas execution test' });
  assert.strictEqual(f.root().metadata.title, 'Canvas execution test');
});
test('set_cover', 'set durable asset reference', async (f) => {
  await f.biz('image', 'cover-node');
  await f.write('set_cover', { cover: { pippitAssetId: 'cover-node' } });
  assert.strictEqual(f.root().metadata.cover.pippitAssetId, 'cover-node');
});
test('set_cover', 'empty cover is a no-op as advertised by schema', async (f) => {
  await f.biz('image', 'cover-node');
  await f.write('set_cover', { cover: { pippitAssetId: 'cover-node' } });
  const description = sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS.find(({ kind }) => kind === 'set_cover')
    .inputSchema.properties.cover.description;
  assert.match(description, /no-op/);
  assert.match(description, /cannot clear/);
  const before = clone(f.store.getState());
  const revision = f.store.getRevision();
  await f.write('set_cover', { cover: {} });
  assert.deepStrictEqual(f.store.getState(), before);
  assert.strictEqual(f.store.getRevision(), revision);
});
test('set_global_bounds', 'world coordinate bounds', async (f) => {
  const bounds = { minX: -5000, minY: -4000, maxX: 5000, maxY: 4000 };
  await f.write('set_global_bounds', { bounds });
  assert.deepStrictEqual(f.root().metadata.globalBounds, bounds);
});
test('create_node', 'registered text node and explicit companion asset', async (f) => {
  await f.nodes();
  assert.strictEqual(f.node('a').type, 'biz/text');
  assert.strictEqual(f.asset('a').content.plainText, 'a');
  assert.strictEqual(f.asset('a').pippitAssetId, 'a');
});
test('update_node_data', 'writes content without changing layout', async (f) => {
  await f.nodes();
  const layout = clone(f.node('a'));
  await f.write('update_node_data', { id: 'a', patch: { plainText: 'Edited text' } });
  assert.strictEqual(f.asset('a').content.plainText, 'Edited text');
  assert.deepStrictEqual(f.node('a'), layout);
});
test('set_node_style', 'style round trip', async (f) => {
  await f.nodes();
  const style = { background: '#123456', borderRadius: 8, opacity: 0.6 };
  await f.write('set_node_style', { id: 'a', style });
  for (const [key, value] of Object.entries(style)) assert.strictEqual(f.node('a').style[key], value);
});
test('update_asset', 'content and extra retain siblings', async (f) => {
  await f.nodes();
  await f.write('update_asset', {
    assetId: 'a',
    contentPatch: { placeholder: 'Type here' },
    extraPatch: { audit: 'local' },
  });
  assert.strictEqual(f.asset('a').content.plainText, 'a');
  assert.strictEqual(f.asset('a').content.placeholder, 'Type here');
  assert.strictEqual(f.asset('a').extra.audit, 'local');
});
test('move_nodes', 'selected displacement and untouched sibling', async (f) => {
  await f.nodes();
  const before = clone(f.root().nodes);
  await f.write('move_nodes', { ids: ['a', 'b'], delta: { dx: 35, dy: -20 } });
  for (const id of ['a', 'b']) {
    assert.strictEqual(f.node(id).x, before[id].x + 35);
    assert.strictEqual(f.node(id).y, before[id].y - 20);
  }
  assert.deepStrictEqual(f.node('c'), before.c);
});
test('move_nodes', 'reject malformed x/y delta without corrupting coordinates', async (f) => {
  await f.nodes();
  const before = clone(f.store.getState());
  const revision = f.store.getRevision();
  // Public schema requires dx/dy. The live CLI exposed missing runtime validation;
  // retain this rejection assertion until production actually fixes the defect.
  failure(await f.call('move_nodes', { ids: ['a'], delta: { x: 125, y: 75 } }));
  assert.deepStrictEqual(f.store.getState(), before);
  assert.strictEqual(f.store.getRevision(), revision);
});
test('resize_node', 'pixel dimensions', async (f) => {
  await f.nodes();
  await f.write('resize_node', { id: 'a', size: { w: 220, h: 160 } });
  assert.strictEqual(f.node('a').w, 220);
  assert.strictEqual(f.node('a').h, 160);
});
test('set_node_position', 'absolute position', async (f) => {
  await f.nodes();
  await f.write('set_node_position', { id: 'b', position: { x: 123, y: 456 } });
  assert.deepStrictEqual(f.world('b'), { x: 123, y: 456 });
});
for (const direction of ['left', 'center', 'right', 'top', 'middle', 'bottom']) {
  test('align_nodes', direction, async (f) => {
    const ids = await f.nodes();
    await f.write('align_nodes', { ids, direction });
    const value = (n) =>
      ({
        left: n.x,
        center: n.x + n.w / 2,
        right: n.x + n.w,
        top: n.y,
        middle: n.y + n.h / 2,
        bottom: n.y + n.h,
      })[direction];
    assert.strictEqual(new Set(ids.map((id) => value(f.node(id)))).size, 1);
  });
}
for (const axis of ['horizontal', 'vertical']) {
  test('distribute_nodes', axis, async (f) => {
    const ids = await f.nodes();
    await f.write('distribute_nodes', { ids, axis });
    const coordinate = axis === 'horizontal' ? 'x' : 'y';
    const size = axis === 'horizontal' ? 'w' : 'h';
    const [a, b, c] = ids.map(f.node);
    assert.strictEqual(b[coordinate] - a[coordinate] - a[size], c[coordinate] - b[coordinate] - b[size]);
  });
}
for (const layout of [
  'horizontal',
  'vertical',
  'grid',
  'horizontal-directed-cluster',
  'vertical-directed-cluster',
]) {
  test('arrange_nodes', layout, async (f) => {
    const ids = await f.nodes();
    await f.edge();
    const before = clone(f.root().nodes);
    await f.write('arrange_nodes', {
      ids,
      layout,
      options: { horizontalGap: 32, verticalGap: 24, gridColumns: 2 },
    });
    assert.notDeepStrictEqual(f.root().nodes, before);
    for (const id of ids) {
      assert(Number.isFinite(f.node(id).x) && Number.isFinite(f.node(id).y));
      assert.strictEqual(f.asset(id).content.plainText, id);
    }
    if (layout === 'horizontal') assert.strictEqual(f.node('b').x - f.node('a').x - f.node('a').w, 32);
    if (layout === 'vertical') assert.strictEqual(f.node('b').y - f.node('a').y - f.node('a').h, 24);
  });
}
for (const strategy of [
  'horizontal-directed-cluster',
  'vertical-directed-cluster',
  'dag-layered',
  'component-packing',
  'incremental-stress',
]) {
  test('auto_layout_nodes', strategy, async (f) => {
    const ids = await f.nodes();
    await f.edge();
    await f.edge('second', 'b', 'c');
    const before = clone(f.root().nodes);
    await f.write('auto_layout_nodes', { options: { ids, strategy } });
    assert.notDeepStrictEqual(f.root().nodes, before);
    assert.deepStrictEqual(Object.keys(f.root().edges).sort(), ['edge', 'second']);
    for (const id of ids) assert(Number.isFinite(f.node(id).x) && Number.isFinite(f.node(id).y));
  });
}
test('group_nodes', 'membership, world position and undo redo', async (f) => {
  await f.nodes();
  const before = clone(f.store.getState());
  f.store.clearHistory();
  const positions = ['a', 'b'].map(f.world);
  const id = await f.group();
  assert.deepStrictEqual(f.node(id).children, ['a', 'b']);
  for (const child of ['a', 'b']) assert.strictEqual(f.node(child).parentId, id);
  assert.deepStrictEqual(['a', 'b'].map(f.world), positions);
  const after = clone(f.store.getState());
  f.store.undo();
  assert.deepStrictEqual(f.store.getState(), before);
  f.store.redo();
  assert.deepStrictEqual(f.store.getState(), after);
});
test('ungroup', 'dissolves group and retains children assets and position', async (f) => {
  await f.nodes();
  const id = await f.group();
  const positions = ['a', 'b'].map(f.world);
  await f.write('ungroup', { groupId: id });
  assert.strictEqual(f.node(id), undefined);
  assert.strictEqual(f.asset(id), undefined);
  assert.deepStrictEqual(['a', 'b'].map(f.world), positions);
  for (const child of ['a', 'b']) {
    assert.strictEqual(f.node(child).parentId, null);
    assert(f.asset(child));
  }
});
test('reparent', 'insert and detach with stable world position', async (f) => {
  await f.nodes();
  const id = await f.group();
  const before = f.world('c');
  await f.write('reparent', { nodeId: 'c', newParentId: id, index: 1 });
  assert.deepStrictEqual(f.node(id).children, ['a', 'c', 'b']);
  assert.strictEqual(f.node('c').parentId, id);
  assert.deepStrictEqual(f.world('c'), before);
  await f.write('reparent', { nodeId: 'c', newParentId: null });
  assert(!f.node(id).children.includes('c'));
  assert.deepStrictEqual(f.world('c'), before);
});
test('reorder_children', 'reorders without losing membership', async (f) => {
  await f.nodes();
  const id = await f.group(['a', 'b', 'c']);
  await f.write('reorder_children', { groupId: id, orderedChildren: ['c', 'a', 'b'] });
  assert.deepStrictEqual(f.node(id).children, ['c', 'a', 'b']);
  for (const child of ['a', 'b', 'c']) assert.strictEqual(f.node(child).parentId, id);
});
test('reorder_children', 'invalid membership rolls back', async (f) => {
  await f.nodes();
  const id = await f.group();
  const before = clone(f.store.getState());
  failure(await f.call('reorder_children', { groupId: id, orderedChildren: ['a', 'missing'] }));
  assert.deepStrictEqual(f.store.getState(), before);
});
test('set_group_collapsed', 'collapse expand preserves membership', async (f) => {
  await f.nodes();
  const id = await f.group();
  const before = clone(f.node(id));
  await f.write('set_group_collapsed', { groupId: id, collapsed: true });
  assert.strictEqual(f.node(id).data.collapsed, true);
  assert.deepStrictEqual(f.node(id).children, before.children);
  await f.write('set_group_collapsed', { groupId: id, collapsed: false });
  assert.strictEqual(f.node(id).data.collapsed, false);
  assert.strictEqual(f.node(id).w, before.w);
  assert.strictEqual(f.node(id).h, before.h);
});
test('create_edge', 'reference endpoints', async (f) => {
  await f.nodes();
  await f.edge();
  assert.strictEqual(f.root().edges.edge.source, 'a');
  assert.strictEqual(f.root().edges.edge.target, 'b');
});
test('reconnect_edge', 'replaces endpoints', async (f) => {
  await f.nodes();
  await f.edge();
  await f.write('reconnect_edge', { id: 'edge', patch: { source: 'b', target: 'c' } });
  assert.strictEqual(f.root().edges.edge.source, 'b');
  assert.strictEqual(f.root().edges.edge.target, 'c');
});
test('update_edge_data', 'label edit retains endpoints', async (f) => {
  await f.nodes();
  await f.edge();
  await f.write('update_edge_data', { id: 'edge', patch: { label: 'Uses reference' } });
  assert.strictEqual(f.root().edges.edge.data.label, 'Uses reference');
  assert.strictEqual(f.root().edges.edge.source, 'a');
});
test('delete_edges', 'deletes edge without deleting assets', async (f) => {
  await f.nodes();
  await f.edge();
  await f.write('delete_edges', { ids: ['edge'] });
  assert.strictEqual(f.root().edges.edge, undefined);
  assert(f.node('a') && f.node('b') && f.asset('a') && f.asset('b'));
});
test('delete_nodes', 'cleans incident edges, group membership and undo', async (f) => {
  await f.nodes();
  await f.edge();
  const groupId = await f.group();
  f.store.clearHistory();
  const before = clone(f.store.getState());
  await f.write('delete_nodes', { ids: ['a'] });
  assert.strictEqual(f.node('a'), undefined);
  // Deleted business nodes keep their assets available for material reuse.
  assert.deepStrictEqual(f.asset('a'), before.assets.a);
  assert.strictEqual(f.root().edges.edge, undefined);
  if (f.node(groupId)) assert(!f.node(groupId).children.includes('a'));
  assert(f.asset('b'));
  const after = clone(f.store.getState());
  f.store.undo();
  assert.deepStrictEqual(f.store.getState(), before);
  f.store.redo();
  assert.deepStrictEqual(f.store.getState(), after);
});
test('delete', 'combined node and edge deletion', async (f) => {
  await f.nodes();
  await f.edge();
  await f.edge('second', 'b', 'c');
  await f.write('delete', { nodeIds: ['a'], edgeIds: ['second'] });
  assert.strictEqual(f.node('a'), undefined);
  assert.deepStrictEqual(f.root().edges, {});
  assert(f.node('b') && f.node('c'));
});
test('update_snap_settings', 'partial settings merge', async (f) => {
  await f.write('update_snap_settings', { patch: { threshold: 14, targets: ['guide'] } });
  assert.deepStrictEqual(f.root().settings.snap, { enabled: true, threshold: 14, targets: ['guide'] });
});
test('set_snap_enabled', 'boolean setting', async (f) => {
  await f.write('set_snap_enabled', { enabled: false });
  assert.strictEqual(f.root().settings.snap.enabled, false);
  assert.strictEqual(f.root().settings.snap.threshold, 8);
});
test('set_auto_layout_strategy', 'set and clear preference', async (f) => {
  await f.write('set_auto_layout_strategy', { strategy: 'dag-layered' });
  assert.strictEqual(f.root().settings.autoLayout.strategy, 'dag-layered');
  await f.write('set_auto_layout_strategy', {});
  assert.strictEqual(f.root().settings.autoLayout, undefined);
});
test('apply_patches', 'RFC 6902 array insertion', async (f) => {
  await f.write('apply_patches', {
    patches: [
      {
        assetId: f.store.getState().rootCanvasId,
        op: 'add',
        path: '/content/settings/snap/targets/0',
        value: 'guide',
      },
    ],
  });
  assert.deepStrictEqual(f.root().settings.snap.targets, ['guide', 'grid', 'node']);
});
test('apply_mutations', 'atomic success and one undo step', async (f) => {
  const before = clone(f.store.getState());
  const revision = f.store.getRevision();
  const result = await f.write('apply_mutations', {
    atomic: true,
    intent: 'atomic pair',
    mutations: [
      { kind: 'set_title', title: 'Atomic title' },
      { kind: 'set_snap_enabled', enabled: false },
    ],
  });
  assert.strictEqual(result.data.results.length, 1);
  assert.strictEqual(f.store.getRevision(), revision + 1);
  assert.strictEqual(f.root().metadata.title, 'Atomic title');
  assert.strictEqual(f.root().settings.snap.enabled, false);
  f.store.undo();
  assert.deepStrictEqual(f.store.getState(), before);
});
for (const scenario of ['dryRun', 'revision conflict', 'later invalid patch']) {
  test('apply_mutations', scenario, async (f) => {
    const before = clone(f.store.getState());
    const revision = f.store.getRevision();
    const request = {
      atomic: true,
      intent: scenario,
      mutations: [{ kind: 'set_title', title: 'Must remain isolated' }],
    };
    if (scenario === 'dryRun') request.dryRun = true;
    if (scenario === 'revision conflict') request.expectedRevision = revision + 1;
    if (scenario === 'later invalid patch')
      request.mutations.push({
        kind: 'apply_patches',
        patches: [{ assetId: f.store.getState().rootCanvasId, op: 'remove', path: '' }],
      });
    const result = await f.call('apply_mutations', request);
    if (scenario === 'dryRun') {
      success(result);
      assert.strictEqual(result.data.dryRun, true);
    } else failure(result);
    assert.deepStrictEqual(f.store.getState(), before);
    assert.strictEqual(f.store.getRevision(), revision);
    assert.strictEqual(f.store.canUndo(), false);
  });
}

const businessKinds = sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS.find(
  ({ kind }) => kind === 'create_biz_node',
).inputSchema.properties.nodeKind.enum;
assert.strictEqual(businessKinds.length, 10);
for (const kind of businessKinds) {
  test('create_biz_node', kind, async (f) => {
    await f.write('create_biz_node', { nodeKind: kind, id: 'created', topLeft: { x: 25, y: 40 } });
    assert(f.node('created'));
    assert(f.asset('created'));
    assert.strictEqual(f.node('created').type, f.asset('created').type);
    assert.strictEqual(f.asset('created').pippitAssetId, 'created');
    assert.deepStrictEqual(f.world('created'), { x: 25, y: 40 });
    const content = f.asset('created').content;
    if (kind === 'scene3d') assert(f.asset(content.documentRef.pippitAssetId));
    if (kind === 'timeline-composition') assert(f.asset(content.draftRef.pippitAssetId));
  });
}
test('create_biz_node', 'invalid ratio leaves no assets', async (f) => {
  const before = clone(f.store.getState());
  failure(
    await f.call('create_biz_node', { nodeKind: 'image', id: 'invalid', defaultVideoRatio: 'invalid' }),
    /defaultVideoRatio/,
  );
  assert.deepStrictEqual(f.store.getState(), before);
});

test('get_snapshot', 'full and filtered documents', async (f) => {
  await f.nodes();
  const full = success(await f.call('get_snapshot'));
  assert.deepStrictEqual(full.data.document, JSON.parse(JSON.stringify(f.store.getState())));
  const filtered = success(await f.call('get_snapshot', { assetIds: ['a'], includeRootCanvas: false }));
  assert.deepStrictEqual(Object.keys(filtered.data.document.assets), ['a']);
  const withRoot = success(await f.call('get_snapshot', { assetIds: ['a'], includeRootCanvas: true }));
  assert(withRoot.data.document.assets[f.store.getState().rootCanvasId]);
});
test('get_asset', 'selected asset and missing id', async (f) => {
  await f.nodes();
  assert.deepStrictEqual(success(await f.call('get_asset', { assetId: 'a' })).data.asset, f.asset('a'));
  const before = clone(f.store.getState());
  failure(await f.call('get_asset', { assetId: 'missing' }));
  assert.deepStrictEqual(f.store.getState(), before);
});
test('get_permissions', 'readOnly and permission denial', async (f) => {
  assert.strictEqual(success(await f.call('get_permissions')).data.readOnly, false);
  // Setup only: existing readOnly setting is owned by the document.
  f.store.transact((draft) => {
    draft.assets[draft.rootCanvasId].content.settings.readOnly = true;
  });
  assert.strictEqual(success(await f.call('get_permissions')).data.readOnly, true);
  const before = clone(f.store.getState());
  failure(await f.call('set_title', { title: 'Denied' }), /read.only/i);
  const denied = sdk.createXyqCanvasOpencodeToolDefinitions({
    schema: createSchemaFactory(),
    runtime: { store: f.store, permissions: [] },
  });
  failure(await f.call('get_permissions', {}, denied));
  assert.deepStrictEqual(f.store.getState(), before);
});
for (const command of ['create_checkpoint', 'list_checkpoints', 'compare_checkpoint', 'restore_checkpoint']) {
  test(command, 'checkpoint captures and restores actual assets', async (f) => {
    await f.nodes();
    const before = clone(f.store.getState());
    const checkpoint = success(await f.call('create_checkpoint', { reason: 'before canvas edit' }));
    const checkpointId = checkpoint.data.checkpointId;
    const list = success(await f.call('list_checkpoints'));
    assert.strictEqual(list.data.checkpoints.length, 1);
    assert.strictEqual(list.data.checkpoints[0].checkpointId, checkpointId);
    await f.write('update_node_data', { id: 'a', patch: { plainText: 'After checkpoint' } });
    await f.write('delete_nodes', { ids: ['b'] });
    const diff = success(await f.call('compare_checkpoint', { checkpointId }));
    assert(diff.data.changedAssetIds.includes('a'));
    assert(!diff.data.changedAssetIds.includes('b'), 'deleting a node retains its reusable companion asset');
    assert.strictEqual(f.node('b'), undefined);
    assert.deepStrictEqual(f.asset('b'), before.assets.b);
    assert(diff.data.changedAssetIds.includes(f.store.getState().rootCanvasId));
    f.store.clearHistory();
    success(await f.call('restore_checkpoint', { checkpointId }));
    assert.deepStrictEqual(
      JSON.parse(JSON.stringify(f.store.getState())),
      JSON.parse(JSON.stringify(before)),
    );
    if (command === 'compare_checkpoint') {
      // Restoring the middle node changes object insertion order, not document
      // meaning. Keep comparison's false positive attributed to that command.
      assert.deepStrictEqual(
        success(await f.call('compare_checkpoint', { checkpointId })).data.changedAssetIds,
        [],
      );
    }
  });
}
test('restore_checkpoint', 'missing checkpoint cannot alter the document', async (f) => {
  const before = clone(f.store.getState());
  failure(await f.call('restore_checkpoint', { checkpointId: 'missing' }));
  assert.deepStrictEqual(f.store.getState(), before);
});

const roleDescription = 'xyq.role.updateDescription';
for (const [field, value, inspect] of [
  ['characterName', 'Character', (c) => c.basicInfo.name],
  ['appearanceName', 'Costume', (c) => c.basicInfo.appearanceName],
  ['appearance', 'A blue coat', (c) => c.appearanceInfo.description],
  ['episodes', '1, 2、2 3', (c) => c.episodes],
  ['voice', 'Calm voice', (c) => c.voiceInfo.description],
  ['voiceType', 1, (c) => c.voiceInfo.voice_type],
  [
    'voiceAudio',
    { assetId: 'voice-cloud', pippitAssetId: 'voice-media', durationMs: 4000, name: 'Voice' },
    (c) => c.voiceInfo.audioPippitAssetId,
  ],
]) {
  test(roleDescription, field, async (f) => {
    await f.biz('role');
    await f.write(roleDescription, { nodeId: 'role', assetId: 'role', field, value });
    const expected = field === 'episodes' ? ['1', '2', '3'] : field === 'voiceAudio' ? 'voice-media' : value;
    assert.deepStrictEqual(inspect(f.asset('role').content), expected);
    if (field === 'characterName') assert.strictEqual(f.node('role').data.name, value);
    if (field === 'voiceAudio')
      assert.strictEqual(f.asset('role').content.voiceInfo.audioDurationMs, value.durationMs);
  });
}
test(roleDescription, 'stale target identity rolls back', async (f) => {
  await f.biz('role');
  const before = clone(f.store.getState());
  failure(
    await f.call(roleDescription, {
      nodeId: 'role',
      assetId: 'other-role',
      field: 'characterName',
      value: 'Wrong target',
    }),
  );
  assert.deepStrictEqual(f.store.getState(), before);
});
const appearance = 'xyq.role.updateAppearance';
test(appearance, 'field merge and expected-state conflict', async (f) => {
  await f.biz('role');
  await f.write(appearance, {
    nodeId: 'role',
    assetId: 'role',
    patch: {
      characterName: 'Hero',
      appearanceName: 'Rain coat',
      appearanceDescription: 'Blue coat',
      episodes: ['2', '2', ' 3 '],
      generationPrompt: 'Portrait',
      voice: { description: 'Quiet', voiceType: 1 },
    },
  });
  const c = f.asset('role').content;
  assert.strictEqual(c.basicInfo.name, 'Hero');
  assert.strictEqual(c.basicInfo.appearanceName, 'Rain coat');
  assert.strictEqual(c.appearanceInfo.description, 'Blue coat');
  assert.deepStrictEqual(c.episodes, ['2', '3']);
  assert.strictEqual(c.generation.prompt, 'Portrait');
  assert.strictEqual(c.voiceInfo.description, 'Quiet');
  await f.write(appearance, {
    nodeId: 'role',
    assetId: 'role',
    expected: { characterName: 'Stale name' },
    patch: { characterName: 'Overwrite', appearanceName: 'Updated coat' },
  });
  assert.strictEqual(f.asset('role').content.basicInfo.name, 'Hero');
  assert.strictEqual(f.asset('role').content.basicInfo.appearanceName, 'Updated coat');
});
const sourceCommand = 'xyq.role.updateSourceInfos';
async function sourceFixture(f) {
  await f.biz('role');
  for (const id of ['source-a', 'source-b'])
    await f.write(sourceCommand, {
      nodeId: 'role',
      assetId: 'role',
      operation: {
        type: 'upsert',
        sourceInfo: { type: 'image', pippitAssetId: id, assetId: `${id}-cloud`, name: id },
      },
    });
}
for (const type of ['upsert', 'reorder', 'setDefault', 'setMain', 'setName', 'delete']) {
  test(sourceCommand, type, async (f) => {
    await sourceFixture(f);
    const operations = {
      upsert: {
        type,
        sourceInfo: { type: 'video', pippitAssetId: 'source-video', assetId: 'video-cloud', isMain: true },
      },
      reorder: { type, activePippitAssetId: 'source-b', overPippitAssetId: 'source-a' },
      setDefault: { type, pippitAssetId: 'source-b', isDefault: true },
      setMain: { type, pippitAssetId: 'source-b' },
      setName: { type, pippitAssetId: 'source-b', name: 'New name' },
      delete: { type, pippitAssetId: 'source-a' },
    };
    const before = clone(f.store.getState());
    await f.write(sourceCommand, { nodeId: 'role', assetId: 'role', operation: operations[type] });
    const c = f.asset('role').content;
    if (type === 'upsert') {
      assert.strictEqual(c.sourceInfos['source-video'].type, 'video');
      assert.strictEqual(c.sourceInfos['source-video'].isMain, false);
    }
    if (type === 'reorder') assert(c.sourceInfos['source-b'].order < c.sourceInfos['source-a'].order);
    if (type === 'setDefault') assert.deepStrictEqual(f.store.getState(), before);
    if (type === 'setMain' || type === 'delete')
      assert.strictEqual(c.appearanceInfo.pippitAssetId, 'source-b');
    if (type === 'setName') assert.strictEqual(c.sourceInfos['source-b'].name, 'New name');
    if (type === 'delete') assert.strictEqual(c.sourceInfos['source-a'], undefined);
  });
}
const sceneDescription = 'xyq.scene.updateDescription';
for (const [field, value, inspect] of [
  ['sceneName', 'Forest', (c) => c.basicInfo.name],
  ['sceneViewName', 'Night forest', (c) => c.basicInfo.appearanceName],
  ['description', 'Trees at night', (c) => c.appearanceInfo.description],
  ['episodes', ['1', '2'], (c) => c.episodes],
]) {
  test(sceneDescription, field, async (f) => {
    await f.biz('scene');
    await f.write(sceneDescription, { nodeId: 'scene', assetId: 'scene', field, value });
    assert.deepStrictEqual(inspect(f.asset('scene').content), value);
    if (field === 'sceneName') assert.strictEqual(f.node('scene').data.name, value);
  });
}

const promptCommand = 'xyq.generation.update_prompt';
test(promptCommand, 'node reference, standalone upload, noop and clear', async (f) => {
  await f.biz('image', 'target', {
    caption: 'Keep caption',
    generation: { prompt: 'Before', model: 'existing-model' },
  });
  await f.biz('image', 'reference', { pippitAssetId: 'reference-media', assetId: 'reference-cloud' });
  const tagged = 'Use <node-asset>reference</node-asset>';
  await f.write(promptCommand, { nodeId: 'target', prompt: tagged });
  assert.strictEqual(f.asset('target').content.generation.prompt, tagged);
  assert.strictEqual(f.asset('target').content.caption, 'Keep caption');
  assert.strictEqual(f.asset('target').content.generation.model, 'existing-model');
  assert(
    Object.values(f.root().edges).some(
      (e) => e.type === 'reference' && e.source === 'reference' && e.target === 'target',
    ),
  );
  const revision = f.store.getRevision();
  await f.write(promptCommand, { nodeId: 'target', prompt: tagged });
  assert.strictEqual(f.store.getRevision(), revision);
  // Existing Web draft protocol; this uploaded asset deliberately has no node.
  f.store.commands.updateNodeData('target', (draft) => {
    draft.generation.references = [
      { type: 'image', pippitAssetId: 'uploaded-media', assetId: 'uploaded-cloud', label: 'Uploaded image' },
    ];
  });
  const standalone = 'Use <pippit-asset-id>uploaded-media</pippit-asset-id>';
  await f.write(promptCommand, { nodeId: 'target', prompt: standalone });
  assert.strictEqual(f.asset('target').content.generation.prompt, standalone);
  assert.strictEqual(f.node('uploaded-media'), undefined);
  assert.strictEqual(f.asset('target').content.generation.references[0].assetId, 'uploaded-cloud');
  await f.write(promptCommand, { nodeId: 'target', prompt: '' });
  assert.strictEqual(f.asset('target').content.generation.prompt, '');
  assert.strictEqual(f.asset('target').content.generation.references.length, 1);
});
for (const scenario of ['dryRun', 'unresolved reference', 'missing generation initialization']) {
  test(promptCommand, scenario, async (f) => {
    await f.biz('video', 'target');
    await f.biz('image', 'reference', { pippitAssetId: 'reference-media', assetId: 'reference-cloud' });
    const before = clone(f.store.getState());
    const revision = f.store.getRevision();
    const text =
      scenario === 'unresolved reference'
        ? '<pippit-asset-id>missing-media</pippit-asset-id>'
        : '<node-asset>reference</node-asset>';
    const result = await f.call(promptCommand, {
      nodeId: 'target',
      prompt: text,
      ...(scenario === 'dryRun' ? { dryRun: true } : {}),
    });
    if (scenario === 'unresolved reference') failure(result, /UNRESOLVED_PROMPT_REFERENCE/);
    else success(result);
    if (scenario === 'missing generation initialization')
      assert.strictEqual(f.asset('target').content.generation.prompt, text);
    else {
      assert.deepStrictEqual(f.store.getState(), before);
      assert.strictEqual(f.store.getRevision(), revision);
    }
  });
}

async function main() {
  for (const [index, entry] of cases.entries()) {
    const f = fixture(index);
    try {
      await entry.execute(f);
      assert.strictEqual(networkCalls, 0, 'No operation may use network');
      records.push({ command: entry.command, scenario: entry.scenario, status: 'PASS' });
      console.log(`PASS ${entry.command} / ${entry.scenario}`);
    } catch (error) {
      records.push({
        command: entry.command,
        scenario: entry.scenario,
        status: 'FAIL',
        error: error.message,
      });
      console.error(`FAIL ${entry.command} / ${entry.scenario}: ${error.stack}`);
    } finally {
      f.standalone.canvas.dispose();
    }
  }
  const expected = [
    ...mutationNames,
    ...registered.map(({ name }) => name).filter((name) => !excluded.has(name)),
    'apply_mutations',
    'get_snapshot',
    'get_asset',
    'get_permissions',
    'create_checkpoint',
    'list_checkpoints',
    'compare_checkpoint',
    'restore_checkpoint',
  ];
  const commandResults = expected.map((command) => {
    const matching = records.filter((entry) => entry.command === command);
    return {
      command,
      status:
        matching.length === 0
          ? 'BLOCKED'
          : matching.some((entry) => entry.status === 'FAIL')
            ? 'FAIL'
            : 'PASS',
      scenarios: matching.length,
    };
  });
  const summary = {
    commands: commandResults,
    totalScenarios: records.length,
    passed: records.filter(({ status }) => status === 'PASS').length,
    failed: records.filter(({ status }) => status === 'FAIL').length,
    excluded: [...excluded],
    networkCalls,
  };
  console.log(JSON.stringify(summary, null, 2));
  assert.strictEqual(expected.length, 43, 'Coverage must track every public base/subject/prompt command');
  if (commandResults.some(({ status }) => status !== 'PASS')) process.exitCode = 1;
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

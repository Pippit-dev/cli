'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const { createHash } = require('crypto');
const { createSchemaFactory, ensureNodeRuntimeGlobals } = require('./canvas-command');

const COMMAND = 'xyq.scene3d.apply';
const NODE_ID = 'director';
const vector = (x, y, z) => ({ x, y, z });
const operation = (command, args) => ({ command, args });
const pathData = {
  source: 'click',
  curve: 'polyline',
  points: [
    { id: 'point-start', position: vector(0, 0, 0) },
    { id: 'point-end', position: vector(4, 0, 0) },
  ],
};
const keyframe = (id = 'key-a', frame = 10) =>
  operation('upsert_keyframe', {
    id,
    frame,
    target: { type: 'node', nodeId: 'camera-a' },
    dataPath: 'camera.fov',
    arrayIndex: 0,
    value: 50,
    interpolation: 'hold',
  });
const findNode = (document, id) => document.nodes.find((node) => node.id === id);
const curves = (document) => document.timeline?.animation.fcurves || [];
const keys = (document) => curves(document).flatMap((curve) => curve.keyframes);
const clips = (document) => document.timeline?.animation.motionClips || [];
const approx = (actual, expected) => assert(Math.abs(actual - expected) < 1e-7, `${actual} != ${expected}`);

// Fixture only: the public data command edits existing character-motion clips but cannot import one.
// This is the DTO used by xyq-scene3d-sdk/src/tests/scene3d-sdk.test.ts motion range/reorder/split cases
// and src/character-motion/timeline-clip-model.ts:createDirectorTimelineMotionClip. No FBX is fetched.
function motionFixture(id, frameStart) {
  return {
    id,
    target: { type: 'node', nodeId: 'character-a' },
    motion: {
      assetId: 'fixture-motion',
      name: 'Fixture motion',
      url: 'motions/fixture.fbx',
      source: 'official',
      sourceRig: 'mixamorig',
      loop: false,
      inPlace: false,
      speed: 1,
      time: 0,
    },
    frameStart,
    frameEnd: frameStart + 30,
    sourceDuration: 2,
    source: 'semantic',
  };
}

async function createFixture(sdk, setup = {}) {
  let allocations = 0;
  const runtime = sdk.createXyqCanvasCommandRuntime({
    canvasId: 'compiled-scene3d-operations',
    allocateAssetId: async () => `fixture-curves-${++allocations}`,
    transportFactory() {
      throw new Error('Compiled document operations must not use network');
    },
  });
  const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
    schema: createSchemaFactory(),
    runtime: {
      store: runtime.store,
      permissions: ['canvas.read', 'canvas.asset.read', 'canvas.write', 'canvas.asset.write'],
    },
  });
  const invoke = async (input) =>
    JSON.parse(
      await tools.apply_mutations.execute({
        atomic: true,
        intent: 'Compiled Scene3D operation test',
        mutations: [{ kind: 'invoke_command', name: COMMAND, args: [input] }],
      }),
    );
  async function query() {
    const result = JSON.parse(await tools['xyq.scene3d.query'].execute({ nodeId: NODE_ID }));
    assert.strictEqual(result.ok, true, JSON.stringify(result));
    return result.data;
  }
  async function apply(operations, dryRun = false) {
    const input = { nodeId: NODE_ID, operations, ...(dryRun ? { dryRun: true } : {}) };
    await runtime.prepareCommand(COMMAND, input);
    const result = await invoke(input);
    assert.strictEqual(result.ok, true, JSON.stringify(result));
    if (!dryRun) assert.strictEqual(result.data.results[0].status, 'committed', JSON.stringify(result));
    return (await query()).document;
  }
  try {
    const created = JSON.parse(
      await tools.apply_mutations.execute({
        atomic: true,
        intent: 'Fixture setup (not operation coverage)',
        mutations: [{ kind: 'create_biz_node', nodeKind: 'scene3d', id: NODE_ID }],
      }),
    );
    assert.strictEqual(created.ok, true, JSON.stringify(created));
    await apply([
      operation('create_node', { kind: 'camera', id: 'camera-a', camera: { fov: 40 } }),
      operation('create_node', { kind: 'camera', id: 'camera-b', camera: { fov: 60 } }),
      operation('create_node', {
        kind: 'character',
        id: 'character-a',
        transform: { position: vector(-1, 0, 0) },
      }),
      operation('create_node', {
        kind: 'character',
        id: 'character-b',
        transform: { position: vector(1, 0, 0) },
      }),
      operation('create_node', { kind: 'prop', id: 'prop-a', name: 'Original prop' }),
      operation('create_node', { kind: 'path', id: 'path-a', path: pathData }),
    ]);
    if (setup.key) await apply([keyframe()]);
    if (setup.group)
      await apply([
        operation('group_nodes', { nodeIds: ['character-a', 'character-b'], name: 'Fixture group' }),
      ]);
    const { documentAssetId } = await query();
    const fixtureResult = runtime.store.transact(
      (draft) => {
        draft.assets.unrelated = {
          pippitAssetId: 'unrelated',
          type: 'fixture',
          content: { owner: 'other-feature' },
          extra: {},
        };
        const asset = draft.assets[documentAssetId];
        asset.extra.fixtureMarker = 'preserve-document-metadata';
        if (setup.motion) {
          asset.content.timeline = {
            version: 1,
            fps: 30,
            frameStart: 0,
            frameEnd: 100,
            animation: {
              fcurves: [],
              motionClips: [motionFixture('motion-a', 0), motionFixture('motion-b', 30)],
            },
          };
        }
      },
      { intent: 'Fixture metadata (not operation coverage)' },
    );
    assert.strictEqual(fixtureResult.status, 'committed');
    const primaryBefore = JSON.stringify(runtime.store.getState().assets[NODE_ID]);
    return {
      runtime,
      tools,
      apply,
      invoke,
      query,
      documentAssetId,
      allocations: () => allocations,
      snapshot: () => JSON.stringify(runtime.store.getState()),
      document: async () => (await query()).document,
      assertIsolation() {
        const state = runtime.store.getState();
        assert.deepStrictEqual(state.assets.unrelated.content, { owner: 'other-feature' });
        assert.strictEqual(JSON.stringify(state.assets[NODE_ID]), primaryBefore);
        assert.strictEqual(state.assets[documentAssetId].extra.fixtureMarker, 'preserve-document-metadata');
      },
    };
  } catch (error) {
    runtime.canvas.dispose();
    throw error;
  }
}

const cases = {
  create_node: {
    async run(context) {
      const inputs = [
        { kind: 'camera', id: 'created-camera', camera: { fov: 35 } },
        { kind: 'character', id: 'created-character', gender: 'female' },
        { kind: 'prop', id: 'created-prop', color: '#abcdef' },
        {
          kind: 'primitive',
          id: 'created-primitive',
          primitiveKind: 'SphereGeometry',
          parameters: { radius: 0.8 },
        },
        { kind: 'light', id: 'created-light', lightKind: 'point', intensity: 2 },
        { kind: 'path', id: 'created-path', path: pathData },
      ];
      const before = await context.document();
      const after = await context.apply(inputs.map((args) => operation('create_node', args)));
      assert.strictEqual(after.nodes.length, before.nodes.length + inputs.length);
      for (const input of inputs) assert.strictEqual(findNode(after, input.id).type, input.kind);
      assert.strictEqual(findNode(after, 'created-camera').camera.fov, 35);
      assert.strictEqual(findNode(after, 'created-primitive').primitive.parameters.radius, 0.8);
      assert.strictEqual(findNode(after, 'created-light').light.intensity, 2);
      assert.deepStrictEqual(
        findNode(after, 'created-path').path.points.map((point) => point.position),
        pathData.points.map((point) => point.position),
      );
    },
  },
  remove_node: {
    async run(context) {
      const before = await context.document();
      const after = await context.apply([operation('remove_node', { nodeId: 'prop-a' })]);
      assert.strictEqual(findNode(after, 'prop-a'), undefined);
      assert.strictEqual(after.nodes.length, before.nodes.length - 1);
    },
  },
  rename_node: {
    async run(context) {
      const after = await context.apply([
        operation('rename_node', { nodeId: 'prop-a', name: 'Renamed prop' }),
      ]);
      assert.strictEqual(findNode(after, 'prop-a').name, 'Renamed prop');
    },
  },
  update_transform: {
    async run(context) {
      const transform = { position: vector(3, 1, 2), rotation: vector(0, 90, 0), scale: vector(1, 2, 1) };
      const after = await context.apply([operation('update_transform', { nodeId: 'prop-a', transform })]);
      assert.deepStrictEqual(findNode(after, 'prop-a').transform, transform);
    },
  },
  update_camera: {
    async run(context) {
      const camera = { fov: 42, near: 0.2, far: 1200, lookAt: vector(0, 1, 0) };
      const after = await context.apply([operation('update_camera', { nodeId: 'camera-a', camera })]);
      for (const [key, value] of Object.entries(camera))
        assert.deepStrictEqual(findNode(after, 'camera-a').camera[key], value);
    },
  },
  set_active_camera: {
    async run(context) {
      const before = await context.document();
      const nodeId = before.activeShotCameraNodeId === 'camera-b' ? 'camera-a' : 'camera-b';
      const after = await context.apply([operation('set_active_camera', { nodeId })]);
      assert.strictEqual(after.activeShotCameraNodeId, nodeId);
    },
  },
  toggle_visible: {
    async run(context) {
      const before = findNode(await context.document(), 'prop-a').visible;
      const after = await context.apply([operation('toggle_visible', { nodeId: 'prop-a' })]);
      assert.strictEqual(findNode(after, 'prop-a').visible, !before);
    },
  },
  toggle_lock: {
    async run(context) {
      const before = findNode(await context.document(), 'prop-a').locked;
      const after = await context.apply([operation('toggle_lock', { nodeId: 'prop-a' })]);
      assert.strictEqual(findNode(after, 'prop-a').locked, !before);
    },
  },
  group_nodes: {
    async run(context) {
      const after = await context.apply([
        operation('group_nodes', { nodeIds: ['character-a', 'character-b'], name: 'Agents group' }),
      ]);
      const group = after.nodes.find((node) => node.type === 'group');
      assert(group);
      assert.strictEqual(group.name, 'Agents group');
      assert.deepStrictEqual(group.children, ['character-a', 'character-b']);
      assert.strictEqual(group.group.kind, 'character');
      for (const id of group.children) assert.strictEqual(findNode(after, id).parentId, group.id);
    },
  },
  ungroup_node: {
    setup: { group: true },
    async run(context) {
      const group = (await context.document()).nodes.find((node) => node.type === 'group');
      const after = await context.apply([operation('ungroup_node', { nodeId: group.id })]);
      assert.strictEqual(findNode(after, group.id), undefined);
      for (const id of group.children) assert(!findNode(after, id).parentId);
    },
  },
  update_group_layout: {
    setup: { group: true },
    async run(context) {
      const group = (await context.document()).nodes.find((node) => node.type === 'group');
      const layout = { perRow: 2, perColumn: 1, spacingWidth: 300, spacingDepth: 200 };
      const after = await context.apply([operation('update_group_layout', { nodeId: group.id, layout })]);
      assert.deepStrictEqual(findNode(after, group.id).group.layout, layout);
      approx(
        Math.abs(
          findNode(after, 'character-a').transform.position.x -
            findNode(after, 'character-b').transform.position.x,
        ),
        3,
      );
    },
  },
  upsert_keyframe: {
    async run(context) {
      const after = await context.apply([keyframe()]);
      const key = keys(after).find((point) => point.id === 'key-a');
      assert.strictEqual(key.frame, 10);
      assert.strictEqual(key.value, 50);
      assert.strictEqual(key.interpolation, 'hold');
      const state = context.runtime.store.getState();
      const ref = state.assets[context.documentAssetId].content.timeline.animation.fcurvesRef;
      assert(ref.pippitAssetId);
      assert.strictEqual(state.assets[ref.pippitAssetId].content.encoding, 'compact-v1');
      const updated = await context.apply([operation('upsert_keyframe', { ...keyframe().args, value: 65 })]);
      assert.strictEqual(keys(updated).filter((point) => point.id === 'key-a').length, 1);
      assert.strictEqual(keys(updated).find((point) => point.id === 'key-a').value, 65);
    },
  },
  remove_keyframes: {
    setup: { key: true },
    async run(context) {
      const after = await context.apply([operation('remove_keyframes', { keyframeIds: ['key-a'] })]);
      assert(!keys(after).some((point) => point.id === 'key-a'));
    },
  },
  move_keyframes: {
    setup: { key: true },
    async run(context) {
      const after = await context.apply([
        operation('move_keyframes', { keyframeIds: ['key-a'], deltaFrames: 5 }),
      ]);
      const key = keys(after).find((point) => point.id === 'key-a');
      assert.strictEqual(key.frame, 15);
      assert.strictEqual(key.value, 50);
    },
  },
  set_keyframe_interpolation: {
    setup: { key: true },
    async run(context) {
      const after = await context.apply([
        operation('set_keyframe_interpolation', { keyframeIds: ['key-a'], interpolation: 'linear' }),
      ]);
      assert.strictEqual(keys(after).find((point) => point.id === 'key-a').interpolation, 'linear');
    },
  },
  set_frame_range: {
    async run(context) {
      const after = await context.apply([operation('set_frame_range', { frameStart: 5, frameEnd: 180 })]);
      assert.strictEqual(after.timeline.frameStart, 5);
      assert.strictEqual(after.timeline.frameEnd, 180);
    },
  },
  apply_path_motion: {
    async run(context) {
      let after = await context.apply([
        operation('apply_path_motion', {
          pathNodeId: 'path-a',
          targetNodeId: 'camera-a',
          durationFrames: 30,
          startFrame: 10,
          lookAtStrategy: 'path-tangent',
        }),
      ]);
      const position = curves(after).find(
        (curve) =>
          curve.target.nodeId === 'camera-a' &&
          curve.dataPath === 'transform.position' &&
          curve.arrayIndex === 0,
      );
      assert(position && position.keyframes.length >= 2);
      assert.strictEqual(Math.min(...position.keyframes.map((point) => point.frame)), 10);
      assert.strictEqual(Math.max(...position.keyframes.map((point) => point.frame)), 40);
      approx(position.keyframes.find((point) => point.frame === 40).value, 4);
      after = await context.apply([
        operation('apply_path_motion', {
          pathNodeId: 'path-a',
          targetNodeId: 'character-a',
          durationFrames: 30,
          startFrame: 60,
        }),
      ]);
      const clip = after.timeline.animation.pathMotionClips.find(
        (item) => item.target.nodeId === 'character-a',
      );
      assert(clip);
      assert.strictEqual(clip.frameStart, 60);
      assert.strictEqual(clip.frameEnd, 90);
    },
  },
  update_motion_clip: {
    setup: { motion: true },
    async run(context) {
      const after = await context.apply([
        operation('update_motion_clip', { clipId: 'motion-a', patch: { trimStart: 0.5, trimEnd: 1.5 } }),
      ]);
      const clip = clips(after).find((item) => item.id === 'motion-a');
      approx(clip.trimStart, 0.5);
      approx(clip.trimEnd, 1.5);
      assert.strictEqual(clip.motion.assetId, 'fixture-motion');
    },
  },
  remove_motion_clip: {
    setup: { motion: true },
    async run(context) {
      const after = await context.apply([operation('remove_motion_clip', { clipId: 'motion-a' })]);
      assert.deepStrictEqual(
        clips(after).map((clip) => clip.id),
        ['motion-b'],
      );
    },
  },
  split_motion_clip: {
    setup: { motion: true },
    async run(context) {
      const after = await context.apply([operation('split_motion_clip', { clipId: 'motion-a', frame: 15 })]);
      const split = clips(after)
        .filter((clip) => clip.frameStart < 30)
        .sort((a, b) => a.frameStart - b.frameStart);
      assert.deepStrictEqual(
        split.map((clip) => [clip.frameStart, clip.frameEnd]),
        [
          [0, 15],
          [15, 30],
        ],
      );
      assert.strictEqual(new Set(clips(after).map((clip) => clip.id)).size, 3);
      approx(split[0].trimEnd, 0.5);
      approx(split[1].trimStart, 0.5);
    },
  },
  reorder_motion_clip: {
    setup: { motion: true },
    async run(context) {
      const after = await context.apply([
        operation('reorder_motion_clip', { clipId: 'motion-b', insertIndex: 0 }),
      ]);
      const ordered = [...clips(after)].sort((a, b) => a.frameStart - b.frameStart);
      assert.deepStrictEqual(
        ordered.map((clip) => clip.id),
        ['motion-b', 'motion-a'],
      );
      assert.deepStrictEqual(
        ordered.map((clip) => [clip.frameStart, clip.frameEnd]),
        [
          [0, 30],
          [30, 60],
        ],
      );
    },
  },
};

async function rejectionPreservesState(context, operations) {
  const input = { nodeId: NODE_ID, operations };
  const before = context.snapshot();
  const allocated = context.allocations();
  await assert.rejects(() => context.runtime.prepareCommand(COMMAND, input));
  const result = await context.invoke(input);
  assert.strictEqual(result.ok, false, JSON.stringify(result));
  assert.strictEqual(context.snapshot(), before, 'Rejected operation changed the complete CanvasDocument');
  assert.strictEqual(context.allocations(), allocated, 'Rejected operation allocated a curve asset ID');
}

const boundaries = {
  'ordinary camera edit: existing frame updates keyframe, other frame updates base': async (context) => {
    await context.apply([keyframe()]);
    let after = await context.apply([
      operation('update_camera', { nodeId: 'camera-a', frame: 10, camera: { fov: 65 } }),
    ]);
    assert.strictEqual(findNode(after, 'camera-a').camera.fov, 40);
    assert.strictEqual(keys(after).find((point) => point.id === 'key-a').value, 65);
    after = await context.apply([
      operation('update_camera', { nodeId: 'camera-a', frame: 20, camera: { fov: 70 } }),
    ]);
    assert.strictEqual(findNode(after, 'camera-a').camera.fov, 70);
    assert.strictEqual(keys(after).find((point) => point.id === 'key-a').value, 65);
    assert(!keys(after).some((point) => point.frame === 20));
  },
  'dryRun: full execution without document changes or ID allocation': async (context) => {
    const before = context.snapshot();
    const allocated = context.allocations();
    await context.apply([operation('create_node', { kind: 'prop', id: 'dry-run-prop' }), keyframe()], true);
    assert.strictEqual(context.snapshot(), before);
    assert.strictEqual(context.allocations(), allocated);
  },
  'atomic: valid edits and new curve followed by missing object roll back': async (context) => {
    await rejectionPreservesState(context, [
      operation('rename_node', { nodeId: 'prop-a', name: 'Must not persist' }),
      keyframe(),
      operation('remove_node', { nodeId: 'missing-object' }),
    ]);
  },
  'locked object: transform rejected without state changes': async (context) => {
    await context.apply([operation('toggle_lock', { nodeId: 'prop-a' })]);
    await rejectionPreservesState(context, [
      operation('update_transform', { nodeId: 'prop-a', transform: { position: vector(9, 9, 9) } }),
    ]);
  },
  'schema: unknown node kind rejected without state changes': async (context) => {
    await rejectionPreservesState(context, [
      operation('create_node', { id: 'invalid-kind', kind: 'vehicle' }),
    ]);
  },
  'path motion: unsupported prop target rejected': async (context) => {
    await rejectionPreservesState(context, [
      operation('apply_path_motion', { pathNodeId: 'path-a', targetNodeId: 'prop-a' }),
    ]);
  },
};

async function main() {
  ensureNodeRuntimeGlobals();
  const artifactPath = path.resolve(
    process.argv[2] || path.join(__dirname, '../dist/xyq-canvas-command-runtime.cjs'),
  );
  const sdk = require(artifactPath);
  const definition = sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS.find((entry) => entry.name === COMMAND);
  const names = definition.inputSchema.properties.operations.items.oneOf.map(
    (item) => item.properties.command.const,
  );
  const compareNames = (a, b) => a.localeCompare(b, 'en');
  assert.deepStrictEqual(
    [...names].sort(compareNames),
    Object.keys(cases).sort(compareNames),
    'Every compiled Scene3D operation must have an executable test',
  );
  const results = [];
  async function runCase(name, test, scope) {
    let context;
    try {
      context = await createFixture(sdk, test.setup);
    } catch (error) {
      results.push({
        scope,
        operation: name,
        status: 'BLOCKED',
        error: `Fixture setup failed: ${error.message}`,
      });
      return;
    }
    try {
      await test.run(context);
      context.assertIsolation();
      results.push({ scope, operation: name, status: 'PASS' });
    } catch (error) {
      results.push({ scope, operation: name, status: 'FAIL', error: error.stack || error.message });
    } finally {
      context.runtime.canvas.dispose();
    }
  }
  for (const name of names) await runCase(name, cases[name], 'operation');
  for (const [name, run] of Object.entries(boundaries)) await runCase(name, { run }, 'boundary');
  console.log(
    JSON.stringify(
      {
        artifact: artifactPath,
        sha256: createHash('sha256').update(fs.readFileSync(artifactPath)).digest('hex'),
        setupNote:
          'Character motion DTOs are fixture setup only; PASS requires the actual compiled operation plus state assertions.',
        results,
      },
      null,
      2,
    ),
  );
  if (results.some((result) => result.status !== 'PASS')) process.exitCode = 1;
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

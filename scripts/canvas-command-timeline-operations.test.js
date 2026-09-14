'use strict';

const assert = require('assert');
const path = require('path');
const { createSchemaFactory, ensureNodeRuntimeGlobals } = require('./canvas-command');

// Wire fixtures follow timeline-editor-sdk/src/core/defaults.ts and
// tests/core-commands.test.ts. Sources are queried from real Canvas factories;
// no test resource namespace, URL, player or media upload is invented here.
const clone = (value) => JSON.parse(JSON.stringify(value));
const command = (type, payload) => ({ type, payload });
const transform = () => ({
  flipX: false,
  flipY: false,
  rotationDeg: 0,
  scale: { x: 1, y: 1 },
  translation: { x: 0, y: 0 },
});
const frame = () => ({ fit: 'contain', transform: transform() });
const playback = () => ({
  direction: 'forward',
  pitchMode: 'preserve',
  rate: { denominator: 1, numerator: 1 },
});
const audio = () => ({ fadeInDuration: 0, fadeOutDuration: 0, gain: 1, muted: false });
const textStyle = () => ({
  background: { color: '#000000', alpha: 0 },
  bold: false,
  fill: { color: '#FFFFFF', alpha: 1 },
  font: { family: 'sans-serif' },
  fontSizePx: 48,
  italic: false,
  letterSpacing: 0,
  lineSpacing: 0.2,
  stroke: { alpha: 1, color: '#000000', widthPx: 0 },
});
const textLayout = () => ({ align: 'center', centerX: 0.5, centerY: 0.85, maxWidth: 0.9 });
const track = (kind, trackId, order) => ({
  trackId,
  kind,
  order,
  enabled: true,
  locked: false,
  ...(kind === 'audio' ? { role: 'bgm' } : {}),
  ...(kind === 'text' ? { role: 'subtitle', defaultStyle: textStyle(), defaultLayout: textLayout() } : {}),
});
const textClip = (clipId, trackId, start, duration, text) => ({
  clipId,
  trackId,
  kind: 'text',
  enabled: true,
  timelineRange: { start, duration },
  text,
});
const clipsOn = (draft, trackId) =>
  Object.values(draft.clipsById)
    .filter((clip) => clip.trackId === trackId)
    .sort((left, right) => left.timelineRange.start - right.timelineRange.start);

async function createFixture(sdk, options = {}) {
  const runtime = sdk.createXyqCanvasCommandRuntime({
    canvasId: 'timeline-operations-test',
    transportFactory() {
      throw new Error('Timeline document tests must not create a network transport');
    },
  });
  const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
    schema: createSchemaFactory(),
    runtime: {
      store: runtime.store,
      permissions: ['canvas.read', 'canvas.asset.read', 'canvas.write', 'canvas.asset.write'],
    },
  });
  const mutation = async (value) =>
    JSON.parse(
      await tools.apply_mutations.execute({
        atomic: true,
        intent: 'Compiled timeline operation test',
        mutations: [value],
      }),
    );
  const query = async () => {
    const result = JSON.parse(await tools['xyq.timeline.query'].execute({ nodeId: 'timeline' }));
    assert.strictEqual(result.ok, true, `Timeline query failed: ${JSON.stringify(result)}`);
    return result.data;
  };
  const execute = async (commands, extra = {}) => {
    const { draft } = await query();
    return mutation({
      kind: 'invoke_command',
      name: 'xyq.timeline.apply',
      args: [{ nodeId: 'timeline', expectedRevision: draft.revision, commands, ...extra }],
    });
  };
  const apply = async (commands) => {
    const result = await execute(commands);
    assert.strictEqual(result.ok, true, `Timeline apply failed: ${JSON.stringify(result)}`);
    return query();
  };
  try {
    for (const [nodeKind, id, mediaId] of [
      ['video', 'video-source', 'media-video'],
      ['video', 'video-replacement', 'media-video-replacement'],
      ['image', 'image-source', 'media-image'],
      ['audio', 'audio-source', 'media-audio'],
    ]) {
      const result = await mutation({
        kind: 'create_biz_node',
        nodeKind,
        id,
        initialData: { pippitAssetId: mediaId, durationMs: 10000 },
      });
      assert.strictEqual(result.ok, true, `Source factory failed: ${JSON.stringify(result)}`);
    }
    const created = await mutation({
      kind: 'create_biz_node',
      nodeKind: 'timeline-composition',
      id: 'timeline',
    });
    assert.strictEqual(created.ok, true, `Timeline factory failed: ${JSON.stringify(created)}`);
    const initial = await query();
    const primary = initial.draft.primaryVideoTrackId;
    const sources = Object.fromEntries(
      initial.availableCanvasSources.map((source) => [source.nodeId, source]),
    );
    for (const [id, kind] of [
      ['video-source', 'video'],
      ['video-replacement', 'video'],
      ['image-source', 'image'],
      ['audio-source', 'audio'],
    ]) {
      assert.strictEqual(sources[id]?.source.mediaType, kind, `Missing completed Canvas source ${id}`);
      assert.strictEqual(sources[id].source.resourceType, 'pippit-asset');
    }
    const mediaClip = (clipId, trackId, nodeId, start, duration) => {
      const source = sources[nodeId];
      const mediaType = source.source.mediaType;
      return {
        clipId,
        trackId,
        kind: 'media',
        enabled: true,
        sourceRef: source.sourceRef,
        timelineRange: { start, duration },
        ...(mediaType !== 'audio' ? { frame: frame() } : {}),
        ...(mediaType !== 'image'
          ? { audio: audio(), playback: playback(), sourceRange: { start: 0, duration } }
          : {}),
      };
    };
    const insert = (clipId, trackId, nodeId, start, duration) =>
      command('insert_clip', {
        trackId,
        source: clone(sources[nodeId].source),
        clip: mediaClip(clipId, trackId, nodeId, start, duration),
      });
    const setup = [
      command('insert_video_track', { track: track('video', 'video-overlay', 1) }),
      command('insert_video_track', { track: track('video', 'video-empty', 2) }),
      command('insert_audio_track', { track: track('audio', 'audio-main', 0) }),
      command('insert_audio_track', { track: track('audio', 'audio-empty', 1) }),
      insert('video-clip', primary, 'video-source', 0, 4000000),
      insert('image-clip', primary, 'image-source', 4000000, 2000000),
      insert('overlay-clip', 'video-overlay', 'image-source', 1000000, 1000000),
      insert('audio-clip', 'audio-main', 'audio-source', 0, 4000000),
      command('set_draft_extensions', { extensions: { testOwner: { preserve: true } } }),
    ];
    if (options.withText !== false) {
      setup.push(
        command('insert_text_track', { track: track('text', 'subtitles', 0) }),
        command('insert_clip', {
          trackId: 'subtitles',
          clip: { ...textClip('text-a', 'subtitles', 0, 1500000, 'First subtitle'), style: { italic: true } },
        }),
        command('insert_clip', {
          trackId: 'subtitles',
          clip: {
            ...textClip('text-b', 'subtitles', 2000000, 1000000, 'Second subtitle'),
            style: { fontSizePx: 40 },
          },
        }),
      );
    }
    const ready = await apply(setup);
    assert.strictEqual(ready.durationUs, 6000000);
    assert.strictEqual(ready.draft.clipsById['video-clip'].sourceRange.duration, 4000000);
    return {
      runtime,
      tools,
      query,
      execute,
      apply,
      primary,
      sources,
      insert,
      mediaClip,
      dispose: () => runtime.canvas.dispose(),
    };
  } catch (error) {
    runtime.canvas.dispose();
    throw error;
  }
}

const CASES = [
  {
    name: 'insert_clip',
    commands: (f) => [f.insert('inserted-image', f.primary, 'image-source', 6000000, 1000000)],
    verify: (f, before, after) => {
      assert.strictEqual(after.durationUs, 7000000);
      assert.deepStrictEqual(
        after.draft.clipsById['inserted-image'],
        f.mediaClip('inserted-image', f.primary, 'image-source', 6000000, 1000000),
      );
    },
  },
  {
    name: 'remove_clip',
    commands: () => [command('remove_clip', { clipId: 'video-clip' })],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.clipsById['video-clip'], undefined);
      assert.strictEqual(draft.sources[f.sources['video-source'].sourceRef], undefined);
      assert.strictEqual(draft.clipsById['image-clip'].timelineRange.start, 0);
      assert(
        !Object.values(f.runtime.store.getRootCanvasContent().edges).some(
          (edge) => edge.source === 'video-source' && edge.target === 'timeline',
        ),
      );
    },
  },
  {
    name: 'split_clip',
    commands: () => [
      command('split_clip', { clipId: 'video-clip', atTimelineUs: 2000000, rightClipId: 'video-right' }),
    ],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.clipsById['video-clip'].sourceRange, { start: 0, duration: 2000000 });
      assert.deepStrictEqual(draft.clipsById['video-right'].sourceRange, {
        start: 2000000,
        duration: 2000000,
      });
      assert.deepStrictEqual(draft.clipsById['video-right'].timelineRange, {
        start: 2000000,
        duration: 2000000,
      });
      assert.strictEqual(draft.clipsById['image-clip'].timelineRange.start, 4000000);
    },
  },
  {
    name: 'replace_source',
    commands: (f) => [
      command('replace_source', {
        clipId: 'video-clip',
        sourceRef: f.sources['video-replacement'].sourceRef,
        source: clone(f.sources['video-replacement'].source),
        sourceRange: { start: 1000000, duration: 4000000 },
      }),
    ],
    verify: (f, before, { draft }) => {
      const ref = f.sources['video-replacement'].sourceRef;
      assert.strictEqual(draft.clipsById['video-clip'].sourceRef, ref);
      assert.deepStrictEqual(draft.sources[ref], f.sources['video-replacement'].source);
      assert.deepStrictEqual(draft.clipsById['video-clip'].sourceRange, {
        start: 1000000,
        duration: 4000000,
      });
      assert(
        Object.values(f.runtime.store.getRootCanvasContent().edges).some(
          (edge) => edge.source === 'video-replacement' && edge.target === 'timeline',
        ),
      );
    },
  },
  {
    name: 'move_clip',
    commands: () => [command('move_clip', { clipId: 'overlay-clip', start: 2500000 })],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['overlay-clip'].timelineRange, {
        start: 2500000,
        duration: 1000000,
      }),
  },
  {
    name: 'trim_clip',
    commands: () => [
      command('trim_clip', {
        clipId: 'video-clip',
        sourceRange: { start: 500000, duration: 3000000 },
        timelineRange: { start: 0, duration: 3000000 },
      }),
    ],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.clipsById['video-clip'].sourceRange, { start: 500000, duration: 3000000 });
      assert.strictEqual(draft.clipsById['video-clip'].timelineRange.duration, 3000000);
      assert.strictEqual(draft.clipsById['image-clip'].timelineRange.start, 3000000);
    },
  },
  {
    name: 'set_clip_speed',
    commands: () => [
      command('set_clip_speed', { clipId: 'video-clip', rate: { numerator: 2, denominator: 1 } }),
    ],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.clipsById['video-clip'].playback.rate, { numerator: 2, denominator: 1 });
      assert.strictEqual(draft.clipsById['video-clip'].timelineRange.duration, 2000000);
      assert.strictEqual(draft.clipsById['video-clip'].sourceRange.duration, 4000000);
      assert.strictEqual(draft.clipsById['image-clip'].timelineRange.start, 2000000);
    },
  },
  {
    name: 'reorder_video_clips',
    commands: (f) => [
      command('reorder_video_clips', {
        trackId: f.primary,
        orderedClipIds: ['image-clip', 'video-clip'],
        start: 0,
      }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(
        clipsOn(draft, f.primary).map((clip) => [clip.clipId, clip.timelineRange.start]),
        [
          ['image-clip', 0],
          ['video-clip', 2000000],
        ],
      ),
  },
  {
    name: 'set_clip_transform',
    commands: () => [
      command('set_clip_transform', {
        clipId: 'video-clip',
        transform: {
          ...transform(),
          rotationDeg: 15,
          translation: { x: 0.1, y: 0.2 },
          scale: { x: 1.5, y: 1.5 },
        },
      }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['video-clip'].frame.transform, {
        ...transform(),
        rotationDeg: 15,
        translation: { x: 0.1, y: 0.2 },
        scale: { x: 1.5, y: 1.5 },
      }),
  },
  {
    name: 'set_clip_fit',
    commands: () => [command('set_clip_fit', { clipId: 'image-clip', fit: 'cover' })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['image-clip'].frame.fit, 'cover'),
  },
  {
    name: 'set_clip_extensions',
    commands: () => [
      command('set_clip_extensions', {
        clipId: 'image-clip',
        extensions: { annotation: { label: 'Reviewed' } },
      }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['image-clip'].extensions, { annotation: { label: 'Reviewed' } }),
  },
  {
    name: 'set_draft_extensions',
    commands: () => [
      command('set_draft_extensions', {
        extensions: { testOwner: { preserve: true }, review: { approved: false } },
      }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.extensions, {
        testOwner: { preserve: true },
        review: { approved: false },
      }),
  },
  {
    name: 'set_clip_enabled',
    commands: () => [command('set_clip_enabled', { clipId: 'video-clip', enabled: false })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['video-clip'].enabled, false),
  },
  {
    name: 'set_original_audio',
    commands: () => [
      command('set_original_audio', { clipId: 'video-clip', audio: { ...audio(), gain: 0.35, muted: true } }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['video-clip'].audio, { ...audio(), gain: 0.35, muted: true }),
  },
  {
    name: 'move_clip_to_track',
    commands: () => [
      command('move_clip_to_track', { clipId: 'image-clip', targetTrackId: 'video-overlay', start: 3000000 }),
    ],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.clipsById['image-clip'].trackId, 'video-overlay');
      assert.strictEqual(draft.clipsById['image-clip'].timelineRange.start, 3000000);
      assert.deepStrictEqual(
        clipsOn(draft, f.primary).map((clip) => clip.clipId),
        ['video-clip'],
      );
    },
  },
  {
    name: 'set_audio_volume',
    commands: () => [command('set_audio_volume', { clipId: 'audio-clip', gain: 0.4 })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['audio-clip'].audio.gain, 0.4),
  },
  {
    name: 'set_audio_mute',
    commands: () => [command('set_audio_mute', { clipId: 'audio-clip', muted: true })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['audio-clip'].audio.muted, true),
  },
  {
    name: 'set_audio_fade',
    commands: () => [
      command('set_audio_fade', { clipId: 'audio-clip', fadeInDuration: 250000, fadeOutDuration: 500000 }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['audio-clip'].audio, {
        ...audio(),
        fadeInDuration: 250000,
        fadeOutDuration: 500000,
      }),
  },
  {
    name: 'set_track_enabled',
    commands: () => [command('set_track_enabled', { trackId: 'audio-main', enabled: false })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.tracksById['audio-main'].enabled, false),
  },
  {
    name: 'set_track_locked',
    commands: () => [command('set_track_locked', { trackId: 'video-overlay', locked: true })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.tracksById['video-overlay'].locked, true),
  },
  {
    name: 'set_output_size',
    commands: () => [command('set_output_size', { width: 1920, height: 1080 })],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.output, { ...before.draft.output, width: 1920, height: 1080 }),
  },
  {
    name: 'insert_audio_track',
    commands: () => [command('insert_audio_track', { track: track('audio', 'audio-added', 2) })],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.tracksById['audio-added'], track('audio', 'audio-added', 2)),
  },
  {
    name: 'insert_video_track',
    commands: () => [command('insert_video_track', { track: track('video', 'video-added', 3) })],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.tracksById['video-added'], track('video', 'video-added', 3)),
  },
  {
    name: 'remove_audio_track',
    commands: () => [command('remove_audio_track', { trackId: 'audio-empty' })],
    verify: (f, before, { draft }) => {
      assert(before.draft.tracksById['audio-empty']);
      assert.strictEqual(draft.tracksById['audio-empty'], undefined);
      assert(draft.tracksById['audio-main']);
    },
  },
  {
    name: 'remove_video_track',
    commands: () => [command('remove_video_track', { trackId: 'video-empty' })],
    verify: (f, before, { draft }) => {
      assert(before.draft.tracksById['video-empty']);
      assert.strictEqual(draft.tracksById['video-empty'], undefined);
      assert(draft.tracksById[f.primary]);
    },
  },
  {
    name: 'reorder_video_tracks',
    commands: (f) => [
      command('reorder_video_tracks', { orderedTrackIds: [f.primary, 'video-empty', 'video-overlay'] }),
    ],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.tracksById[f.primary].order, 0);
      assert.strictEqual(draft.tracksById['video-empty'].order, 1);
      assert.strictEqual(draft.tracksById['video-overlay'].order, 2);
    },
  },
  {
    name: 'set_primary_video_track',
    commands: () => [command('set_primary_video_track', { trackId: 'video-overlay' })],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.primaryVideoTrackId, 'video-overlay');
      assert.strictEqual(draft.tracksById['video-overlay'].order, 0);
      assert.strictEqual(draft.tracksById[f.primary].order, 1);
      assert.strictEqual(draft.clipsById['overlay-clip'].timelineRange.start, 0);
    },
  },
  {
    name: 'insert_text_track',
    fixture: { withText: false },
    commands: () => [command('insert_text_track', { track: track('text', 'subtitles', 0) })],
    verify: (f, before, { draft }) => {
      assert.strictEqual(before.draft.tracksById.subtitles, undefined);
      assert.deepStrictEqual(draft.tracksById.subtitles, track('text', 'subtitles', 0));
    },
  },
  {
    name: 'remove_text_track',
    commands: () => [command('remove_text_track', { trackId: 'subtitles' })],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.tracksById.subtitles, undefined);
      assert.strictEqual(draft.clipsById['text-a'], undefined);
      assert.strictEqual(draft.clipsById['text-b'], undefined);
      assert(draft.clipsById['video-clip']);
    },
  },
  {
    name: 'set_text_track_default_style',
    commands: () => [
      command('set_text_track_default_style', {
        trackId: 'subtitles',
        style: { ...textStyle(), fontSizePx: 56, bold: true },
      }),
    ],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.tracksById.subtitles.defaultStyle, {
        ...textStyle(),
        fontSizePx: 56,
        bold: true,
      });
      assert.deepStrictEqual(draft.clipsById['text-a'].style, before.draft.clipsById['text-a'].style);
    },
  },
  {
    name: 'set_text_track_default_layout',
    commands: () => [
      command('set_text_track_default_layout', {
        trackId: 'subtitles',
        layout: { ...textLayout(), align: 'start', centerY: 0.7 },
      }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.tracksById.subtitles.defaultLayout, {
        ...textLayout(),
        align: 'start',
        centerY: 0.7,
      }),
  },
  {
    name: 'set_text_clip_text',
    commands: () => [command('set_text_clip_text', { clipId: 'text-a', text: 'Updated subtitle' })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['text-a'].text, 'Updated subtitle'),
  },
  {
    name: 'set_text_clip_enabled',
    commands: () => [command('set_text_clip_enabled', { clipId: 'text-a', enabled: false })],
    verify: (f, before, { draft }) => assert.strictEqual(draft.clipsById['text-a'].enabled, false),
  },
  {
    name: 'set_text_clip_style',
    commands: () => [
      command('set_text_clip_style', { clipId: 'text-a', style: { bold: true, fontSizePx: 56 } }),
    ],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['text-a'].style, { bold: true, fontSizePx: 56 }),
  },
  {
    name: 'set_text_clip_layout',
    commands: () => [command('set_text_clip_layout', { clipId: 'text-a', layout: { centerY: 0.7 } })],
    verify: (f, before, { draft }) =>
      assert.deepStrictEqual(draft.clipsById['text-a'].layout, { centerY: 0.7 }),
  },
  {
    name: 'apply_text_style_to_all',
    commands: () => [command('apply_text_style_to_all', { trackId: 'subtitles', style: { bold: true } })],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.clipsById['text-a'].style, { italic: true, bold: true });
      assert.deepStrictEqual(draft.clipsById['text-b'].style, { fontSizePx: 40, bold: true });
    },
  },
  {
    name: 'replace_text_clips',
    commands: () => [
      command('replace_text_clips', {
        trackId: 'subtitles',
        clipsById: { 'text-new': textClip('text-new', 'subtitles', 1000000, 2000000, 'Replacement cue') },
      }),
    ],
    verify: (f, before, { draft }) => {
      assert.strictEqual(draft.clipsById['text-a'], undefined);
      assert.strictEqual(draft.clipsById['text-b'], undefined);
      assert.deepStrictEqual(
        draft.clipsById['text-new'],
        textClip('text-new', 'subtitles', 1000000, 2000000, 'Replacement cue'),
      );
    },
  },
  {
    name: 'reflow_text_clips',
    commands: () => [
      command('reflow_text_clips', {
        trackId: 'subtitles',
        rangesByClipId: {
          'text-a': { start: 500000, duration: 1000000 },
          'text-b': { start: 3000000, duration: 1000000 },
        },
      }),
    ],
    verify: (f, before, { draft }) => {
      assert.deepStrictEqual(draft.clipsById['text-a'].timelineRange, { start: 500000, duration: 1000000 });
      assert.deepStrictEqual(draft.clipsById['text-b'].timelineRange, { start: 3000000, duration: 1000000 });
      assert.strictEqual(draft.clipsById['text-a'].text, before.draft.clipsById['text-a'].text);
    },
  },
];

async function verifyBoundaries(sdk) {
  const f = await createFixture(sdk);
  try {
    const before = f.runtime.store.getState();
    const canvasRevision = f.runtime.store.getRevision();
    const snapshot = await f.query();
    const commands = [
      command('set_output_size', { width: 640, height: 360 }),
      command('set_audio_volume', { clipId: 'audio-clip', gain: 0.25 }),
    ];
    const dryRun = await f.execute(commands, { dryRun: true });
    assert.strictEqual(dryRun.ok, true, JSON.stringify(dryRun));
    assert.strictEqual(
      f.runtime.store.getState(),
      before,
      'dryRun must preserve the complete Canvas document',
    );
    assert.strictEqual(f.runtime.store.getRevision(), canvasRevision);
    assert.deepStrictEqual(await f.query(), snapshot);
    console.log('PASS boundary/dryRun: real multi-command preview leaves document and revisions unchanged');

    for (const { name, operationCommands, extra, reason } of [
      {
        name: 'stale-revision',
        operationCommands: commands,
        extra: { expectedRevision: snapshot.draft.revision + 10 },
        reason: 'STALE_REVISION',
      },
      {
        name: 'atomic-rollback',
        operationCommands: [...commands, command('remove_clip', { clipId: 'missing-clip' })],
        extra: {},
        reason: 'TARGET_NOT_FOUND',
      },
      {
        name: 'dryRun-validates',
        operationCommands: [command('remove_clip', { clipId: 'missing-clip' })],
        extra: { dryRun: true },
        reason: 'TARGET_NOT_FOUND',
      },
      {
        name: 'unbound-source',
        operationCommands: [
          command('insert_clip', {
            trackId: f.primary,
            source: clone(f.sources['image-source'].source),
            clip: {
              ...f.mediaClip('unbound-clip', f.primary, 'image-source', 6000000, 1000000),
              sourceRef: 'unbound-source',
            },
          }),
        ],
        extra: {},
        reason: 'TIMELINE_SOURCE_NOT_BOUND',
      },
    ]) {
      const result = await f.execute(operationCommands, extra);
      assert.strictEqual(result.ok, false, `${name}: ${JSON.stringify(result)}`);
      assert(
        JSON.stringify(result).includes(reason),
        `${name}: expected ${reason}, received ${JSON.stringify(result)}`,
      );
      assert.strictEqual(f.runtime.store.getState(), before, `${name} changed the Canvas document`);
      assert.strictEqual(f.runtime.store.getRevision(), canvasRevision);
      assert.deepStrictEqual(await f.query(), snapshot);
      console.log(`PASS boundary/${name}: ${reason}, no partial state persisted`);
    }

    await f.apply([command('remove_clip', { clipId: 'video-clip' })]);
    const changed = f.runtime.store.getState();
    assert.strictEqual(f.runtime.store.undo().status, 'committed');
    assert.deepStrictEqual(
      f.runtime.store.getState(),
      before,
      'One undo must restore draft, source and reference edges',
    );
    assert.strictEqual(f.runtime.store.redo().status, 'committed');
    assert.deepStrictEqual(f.runtime.store.getState(), changed, 'Redo must restore the same edit');
    console.log('PASS boundary/undo-redo: draft and Canvas reference edges restored together');

    const beforeReinsert = f.runtime.store.getState();
    const beforeReinsertSnapshot = await f.query();
    const replacementTrack = clone(beforeReinsertSnapshot.draft.tracksById.subtitles);
    const reinserted = await f.apply([
      command('remove_clip', { clipId: 'text-a' }),
      command('remove_clip', { clipId: 'text-b' }),
      command('remove_text_track', { trackId: 'subtitles' }),
      command('insert_text_track', { track: replacementTrack }),
    ]);
    assert.deepStrictEqual(
      reinserted.draft.tracksById.subtitles,
      replacementTrack,
      'Empty-track cleanup must not delete a track reinserted later in the batch',
    );
    assert.strictEqual(reinserted.draft.clipsById['text-a'], undefined);
    assert.strictEqual(reinserted.draft.clipsById['text-b'], undefined);
    assert.deepStrictEqual(
      reinserted.draft.clipsById['image-clip'],
      beforeReinsertSnapshot.draft.clipsById['image-clip'],
    );
    const afterReinsert = f.runtime.store.getState();
    assert.strictEqual(f.runtime.store.undo().status, 'committed');
    assert.deepStrictEqual(f.runtime.store.getState(), beforeReinsert);
    assert.strictEqual(f.runtime.store.redo().status, 'committed');
    assert.deepStrictEqual(f.runtime.store.getState(), afterReinsert);
    console.log('PASS boundary/reinsert-track: a later track insertion survives earlier clip deletions');
  } finally {
    f.dispose();
  }
}

async function main() {
  ensureNodeRuntimeGlobals();
  const artifactPath = path.resolve(
    process.argv[2] || path.join(__dirname, '../dist/xyq-canvas-command-runtime.cjs'),
  );
  const sdk = require(artifactPath);
  const definition = sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS.find(
    (entry) => entry.name === 'xyq.timeline.apply',
  );
  assert(definition, 'Compiled artifact is missing xyq.timeline.apply');
  const operationNames = definition.inputSchema.properties.commands.items.oneOf.map(
    (variant) => variant.properties.type.const,
  );
  assert.strictEqual(CASES.length, 38);
  const compareNames = (left, right) => (left < right ? -1 : left > right ? 1 : 0);
  assert.deepStrictEqual(
    [...operationNames].sort(compareNames),
    CASES.map((test) => test.name).sort(compareNames),
    'Every published operation needs an execution case',
  );
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => {
    throw new Error('Compiled timeline operation tests must not access the network');
  };
  const results = [];
  const started = Date.now();
  try {
    for (const test of CASES) {
      let f;
      try {
        f = await createFixture(sdk, test.fixture);
        const before = await f.query();
        const sourceAssets = Object.fromEntries(
          Object.keys(f.sources).map((id) => [id, clone(f.runtime.store.getState().assets[id])]),
        );
        const commands = test.commands(f);
        assert(
          commands.some(({ type }) => type === test.name),
          `${test.name} was not executed`,
        );
        const after = await f.apply(commands);
        assert.strictEqual(
          after.draft.revision,
          before.draft.revision + 1,
          `${test.name} must commit one Timeline transaction`,
        );
        assert.notDeepStrictEqual(after.draft, before.draft, `${test.name} did not change the draft`);
        test.verify(f, before, after);
        for (const [id, asset] of Object.entries(sourceAssets))
          assert.deepStrictEqual(
            f.runtime.store.getState().assets[id],
            asset,
            `${test.name} changed source asset ${id}`,
          );
        results.push({ operation: test.name, status: 'PASS' });
        console.log(`PASS ${test.name}: executed and queried resulting state`);
      } catch (error) {
        results.push({
          operation: test.name,
          status: f ? 'FAIL' : 'BLOCKED',
          error: error.stack || String(error),
        });
        console.error(`${f ? 'FAIL' : 'BLOCKED'} ${test.name}: ${error.stack || error}`);
      } finally {
        f?.dispose();
      }
    }
    try {
      await verifyBoundaries(sdk);
    } catch (error) {
      results.push({ operation: 'boundaries', status: 'FAIL', error: error.stack || String(error) });
      console.error(`FAIL boundaries: ${error.stack || error}`);
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
  const failures = results.filter(({ status }) => status !== 'PASS');
  console.log(
    JSON.stringify(
      { artifactPath, elapsedMs: Date.now() - started, results, failed: failures.length },
      null,
      2,
    ),
  );
  assert.strictEqual(
    failures.length,
    0,
    'Compiled timeline execution suite has failures; inspect per-operation results',
  );
  console.log(
    'Compiled timeline execution passed: 38/38 operations and 7 state/history boundaries; no network or generation',
  );
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

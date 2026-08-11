import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

import { PLAN_SCHEMA, convertSnapshotToCanvasPlan } from './plan.mjs';

const fixtureURL = new URL('./testdata/snapshot.json', import.meta.url);
const manifestURL = new URL('./testdata/media-manifest.json', import.meta.url);

async function readJson(url) {
  return JSON.parse(await readFile(url, 'utf8'));
}

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

async function testRepresentativeSnapshot() {
  const snapshot = await readJson(fixtureURL);
  const mediaManifest = await readJson(manifestURL);
  const plan = convertSnapshotToCanvasPlan(snapshot, { mediaManifest });

  assert.equal(plan.schema, PLAN_SCHEMA);
  assert.deepEqual(plan.source, {
    provider: 'libtv',
    project_id: 'fixture-project',
    fingerprint: 'sha256:db75e738a45ec45c6753532f1e10bcb89f9aee7370c8c24f0b13e51368e9f2a3',
  });
  assert.equal(plan.title, 'LibTV adapter fixture');
  assert.equal(plan.required_media.length, 2);
  assert.deepEqual(plan.required_media[0], {
    logical_id: 'media:video-1',
    source_node_id: 'video-1',
    file_name: 'input.mp4',
    media_type: 'video',
    url: 'https://media.example.test/input.mp4?token=fixture',
    metadata: {
      byte_size: 1234,
      duration_ms: 2020,
      extension: 'mp4',
      height: 180,
      mime_type: 'video/mp4',
      width: 320,
    },
  });
  assert.equal(plan.required_media[1].file_name, 'input.wav');
  assert.equal(plan.required_media[1].local_path, 'media/input.wav');
  assert.equal(plan.required_media[1].sha256, 'a'.repeat(64));
  assert.equal(plan.required_media[1].url, undefined);
  assert.equal(plan.nodes.length, 3);
  assert.deepEqual(plan.nodes[2].input_node_logical_ids, ['node:video-1']);
  assert.deepEqual(plan.groups[0].child_logical_ids, ['node:video-1', 'node:audio-1']);
  assert.deepEqual(plan.edges[0], {
    logical_id: 'edge:edge-1',
    source_edge_id: 'edge-1',
    type: 'reference',
    source_node_logical_id: 'node:video-1',
    target_node_logical_id: 'node:clip-1',
    source_handle: 'right',
    target_handle: 'left',
  });
  assert.equal(plan.degradations.length, 1);
  assert.equal(plan.degradations[0].code, 'libtv.video_clip.empty_placeholder');

  const serialized = JSON.stringify(plan);
  for (const forbidden of ['must-not-leak', 'assetId', 'pippitAssetId', 'teamId', 'access_key']) {
    assert.equal(serialized.includes(forbidden), false, `plan leaked forbidden field/value ${forbidden}`);
  }
}

async function testDeterminismAndVolatileExportTime() {
  const snapshot = await readJson(fixtureURL);
  const mediaManifest = await readJson(manifestURL);
  const first = convertSnapshotToCanvasPlan(snapshot, { mediaManifest });
  const second = convertSnapshotToCanvasPlan({ ...snapshot, exportedAt: '2099-01-01T00:00:00.000Z' }, { mediaManifest });
  assert.deepEqual(first, second);
  assert.equal(convertSnapshotToCanvasPlan(snapshot, { mediaManifest, title: 'Override title' }).title, 'Override title');

  const rotated = clone(snapshot);
  rotated.nodeDetails[0].detail.data.url[0] = 'https://media.example.test/input.mp4?token=rotated&expires=999';
  rotated.assetReferences[0].url = 'https://media.example.test/input.mp4?signature=another';
  assert.equal(convertSnapshotToCanvasPlan(rotated, { mediaManifest }).source.fingerprint, first.source.fingerprint);
}

async function testImageAndEmptyPlaceholder() {
  const snapshot = await readJson(fixtureURL);
  snapshot.project.nodes.push({
    id: 'image-1',
    name: 'Pending image',
    type: 'image',
    position: { x: 1500, y: 20 },
    width: 320,
    height: 320,
  });
  snapshot.nodeDetails.push({ sourceNodeId: 'image-1', detail: { data: { type: 'image', url: [] } } });
  const mediaManifest = await readJson(manifestURL);
  const placeholder = convertSnapshotToCanvasPlan(snapshot, { mediaManifest });
  assert.equal(placeholder.nodes.find((node) => node.source_node_id === 'image-1').kind, 'image-placeholder');
  assert.equal(placeholder.degradations.some((item) => item.source_node_id === 'image-1'), true);

  mediaManifest.uploads.push({
    sourceNodeId: 'image-1',
    fileName: 'image.png',
    mediaType: 'image',
    relative_path: 'media/image.png',
    sha256: 'B'.repeat(64),
  });
  const imported = convertSnapshotToCanvasPlan(snapshot, { mediaManifest });
  assert.equal(imported.nodes.find((node) => node.source_node_id === 'image-1').kind, 'image');
  assert.deepEqual(imported.required_media.find((item) => item.source_node_id === 'image-1'), {
    logical_id: 'media:image-1',
    source_node_id: 'image-1',
    file_name: 'image.png',
    media_type: 'image',
    local_path: 'media/image.png',
    sha256: 'b'.repeat(64),
    metadata: {},
  });
}

async function testRejectsUnsafeOrInvalidInputs() {
  const snapshot = await readJson(fixtureURL);
  const mediaManifest = await readJson(manifestURL);
  const unsafe = clone(snapshot);
  unsafe.nodeDetails[0].detail.data.url = ['http://media.example.test/input.mp4'];
  unsafe.assetReferences = [];
  assert.throws(() => convertSnapshotToCanvasPlan(unsafe), /must use HTTPS/);

  const dangling = clone(snapshot);
  dangling.project.edges[0].target = 'missing-node';
  assert.throws(() => convertSnapshotToCanvasPlan(dangling), /references a missing node/);

  const unsupported = clone(snapshot);
  unsupported.project.nodes[1].type = 'prompt';
  assert.throws(() => convertSnapshotToCanvasPlan(unsupported), /unsupported LibTV node type/);

  assert.throws(() => convertSnapshotToCanvasPlan(snapshot, { mediaManifest, title: 'x'.repeat(51) }), /must not exceed 50/);

  const badManifest = await readJson(manifestURL);
  badManifest.uploads[1].relative_path = '../input.wav';
  assert.throws(() => convertSnapshotToCanvasPlan(snapshot, { mediaManifest: badManifest }), /invalid local media path/);

  const encodedSlash = clone(snapshot);
  encodedSlash.assetReferences[0].url = 'https://media.example.test/nested%2Fevil.mp4';
  encodedSlash.nodeDetails[0].detail.data.url = [];
  const audioOnlyManifest = clone(mediaManifest);
  audioOnlyManifest.uploads = audioOnlyManifest.uploads.filter((item) => item.sourceNodeId === 'audio-1');
  assert.equal(convertSnapshotToCanvasPlan(encodedSlash, { mediaManifest: audioOnlyManifest }).required_media[0].file_name, 'evil.mp4');
}

await testRepresentativeSnapshot();
await testDeterminismAndVolatileExportTime();
await testImageAndEmptyPlaceholder();
await testRejectsUnsafeOrInvalidInputs();
process.stdout.write('libtv plan adapter tests passed\n');

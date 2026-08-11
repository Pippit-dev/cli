import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { chmod, mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';

import {
  OFFICIAL_ARTIFACTS,
  OFFICIAL_CLI_VERSION,
  artifactFor,
  bootstrapLibTVCLI,
} from './bootstrap.mjs';

const AUDITED_ARTIFACTS = {
  'darwin-arm64': ['libtv-macos-arm64.zip', '95c21012530917da8ce69eb01ebb197f418783904a0dcd16ddcf27efe5139df7', '1abc924df7fb3d3428b890909c78241749136797d3c046c0b810653e9fbf6fdd'],
  'darwin-x64': ['libtv-macos-x64.zip', 'e8dfad868919522cd1e3ea5f437506bdd193684b96cf3db89512130abddb6347', '0248dd94bdbee377f67153883110f6d48e2c1bebeeeb69d1673c30e257c84592'],
  'linux-arm64': ['libtv-linux-arm64.zip', '369b43f5be1d28dbbde7c1b6711ed746bf9bff1028ba794a6dae4fa01bed601c', '1fe47f1d3b56f826e72c4d4a9b452a1538f5d6974d7ee319678685387d14f43f'],
  'linux-x64': ['libtv-linux-x64.zip', 'cf86f462c5aed60f95dca978cc91ece98c60bcfa27337da008ad59953c3ea7da', 'e79ad52170556b44e957174f880c8a69057f668ddd0b9a4011524cac072c31f3'],
  'win32-arm64': ['libtv-windows-arm64.zip', 'a64f14987ba44cf7345451d557a4dfe527db7b343a1c7032ecf23bcc320974ee', '3ccff728c39277d8ad596d8a3b24bbc071579f83f6bc102c4051233cab2734bc'],
  'win32-x64': ['libtv-windows-amd64.zip', '5c5e14b683ebbafba4b2c156be305384bd9f558169d019442e53b2ea04206bd5', 'a607ea1f557cb513f302138e64d86312b9dfa9e7eed9dffc5c07f5559192f3fb'],
};

function sha256(value) {
  return createHash('sha256').update(value).digest('hex');
}

function fakeArtifact(zipBytes, binaryBytes) {
  return {
    'linux-x64': {
      zipName: 'libtv-linux-x64.zip',
      zipSHA256: sha256(zipBytes),
      binarySHA256: sha256(binaryBytes),
      executable: 'libtv',
    },
  };
}

async function testOfficialArtifactMatrix() {
  assert.deepEqual(Object.keys(OFFICIAL_ARTIFACTS).sort(), Object.keys(AUDITED_ARTIFACTS).sort());
  for (const [key, expected] of Object.entries(OFFICIAL_ARTIFACTS)) {
    const [platform, arch] = key.split('-');
    const artifact = artifactFor(platform, arch);
    assert.equal(artifact.url, `https://liblibai-web-static.liblib.cloud/cli/${OFFICIAL_CLI_VERSION}/${expected.zipName}`);
    assert.match(artifact.zipSHA256, /^[0-9a-f]{64}$/);
    assert.match(artifact.binarySHA256, /^[0-9a-f]{64}$/);
    assert.deepEqual(
      [artifact.zipName, artifact.zipSHA256, artifact.binarySHA256],
      AUDITED_ARTIFACTS[key],
    );
    if (!key.startsWith('win32-')) assert.equal(artifact.executable, 'libtv');
  }
  assert.equal(artifactFor('win32', 'x64').executable, 'libtv.exe');
  assert.throws(() => artifactFor('freebsd', 'x64'), /does not support/);
}

async function testVerifiedInstallAndCacheReuse() {
  const root = await mkdtemp(join(tmpdir(), 'pippit-libtv-bootstrap-test-'));
  const zipBytes = Buffer.from('fake pinned zip');
  const binaryBytes = Buffer.from('#!/bin/sh\necho 1.1.3\n');
  const artifacts = fakeArtifact(zipBytes, binaryBytes);
  let downloads = 0;
  const download = async ({ destination }) => {
    downloads += 1;
    await writeFile(destination, zipBytes, { mode: 0o600 });
  };
  const extract = async ({ destination }) => {
    const nested = join(destination, 'libtv-linux-x64');
    await mkdir(nested, { recursive: true });
    await writeFile(join(nested, 'libtv'), binaryBytes, { mode: 0o600 });
  };
  try {
    const binary = await bootstrapLibTVCLI({
      platform: 'linux',
      arch: 'x64',
      artifacts,
      cacheRoot: root,
      download,
      extract,
    });
    assert.equal(await readFile(binary, 'utf8'), binaryBytes.toString());
    assert.equal((await stat(binary)).mode & 0o777, 0o700);
    const metadataPath = join(dirname(binary), 'metadata.json');
    assert.equal((await stat(metadataPath)).mode & 0o777, 0o600);
    assert.equal(JSON.parse(await readFile(metadataPath, 'utf8')).binary_sha256, sha256(binaryBytes));

    const cached = await bootstrapLibTVCLI({
      platform: 'linux',
      arch: 'x64',
      artifacts,
      cacheRoot: root,
      download: async () => { throw new Error('cache was not reused'); },
      extract,
    });
    assert.equal(cached, binary);
    assert.equal(downloads, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

async function testIntegrityFailures() {
  const root = await mkdtemp(join(tmpdir(), 'pippit-libtv-bootstrap-failure-'));
  const zipBytes = Buffer.from('zip');
  const binaryBytes = Buffer.from('binary');
  try {
    await assert.rejects(bootstrapLibTVCLI({
      platform: 'linux', arch: 'x64', artifacts: fakeArtifact(zipBytes, binaryBytes), cacheRoot: '/',
    }), /broad directory/);

    const badZipArtifacts = fakeArtifact(Buffer.from('different'), binaryBytes);
    await assert.rejects(bootstrapLibTVCLI({
      platform: 'linux', arch: 'x64', artifacts: badZipArtifacts, cacheRoot: join(root, 'bad-zip'),
      download: async ({ destination }) => writeFile(destination, zipBytes),
      extract: async () => { throw new Error('must not extract a bad ZIP'); },
    }), /ZIP failed SHA-256/);

    const artifacts = fakeArtifact(zipBytes, binaryBytes);
    await assert.rejects(bootstrapLibTVCLI({
      platform: 'linux', arch: 'x64', artifacts, cacheRoot: join(root, 'bad-binary'),
      download: async ({ destination }) => writeFile(destination, zipBytes),
      extract: async ({ destination }) => {
        await mkdir(join(destination, 'bundle'), { recursive: true });
        await writeFile(join(destination, 'bundle', 'libtv'), 'tampered');
      },
    }), /binary failed SHA-256/);

    const cacheRoot = join(root, 'corrupt-cache');
    const binary = await bootstrapLibTVCLI({
      platform: 'linux', arch: 'x64', artifacts, cacheRoot,
      download: async ({ destination }) => writeFile(destination, zipBytes),
      extract: async ({ destination }) => {
        await mkdir(join(destination, 'bundle'), { recursive: true });
        await writeFile(join(destination, 'bundle', 'libtv'), binaryBytes);
      },
    });
    await writeFile(binary, 'tampered cache');
    await chmod(binary, 0o700);
    await assert.rejects(bootstrapLibTVCLI({
      platform: 'linux', arch: 'x64', artifacts, cacheRoot,
      download: async () => { throw new Error('must not replace corrupt cache'); },
    }), /cached LibTV CLI failed SHA-256/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

await testOfficialArtifactMatrix();
await testVerifiedInstallAndCacheReuse();
await testIntegrityFailures();
process.stdout.write('libtv bootstrap tests passed\n');

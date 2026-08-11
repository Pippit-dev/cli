#!/usr/bin/env node

import { appendFile, mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const args = process.argv.slice(2);
let testConfig = {};
if (process.env.LIBTV_CONFIG_DIR) {
  try {
    testConfig = JSON.parse(await readFile(join(process.env.LIBTV_CONFIG_DIR, 'fake-cli-test.json'), 'utf8'));
  } catch {
    // A missing test config represents an authenticated default fake.
  }
}
const scenario = testConfig.scenario ?? 'authenticated';
const statePath = testConfig.statePath;
const logPath = testConfig.logPath;

if (process.env.THIRD_PARTY_API_KEY || process.env.UNKNOWN_TOKEN || process.env.SSH_AUTH_SOCK) process.exit(91);
if (logPath) await appendFile(logPath, `${JSON.stringify(args)}\n`);

async function stateExists() {
  if (!statePath) return false;
  try {
    await stat(statePath);
    return true;
  } catch {
    return false;
  }
}

function output(value) {
  process.stdout.write(`${typeof value === 'string' ? value : JSON.stringify(value)}\n`);
}

if (args[0] === '--version') {
  output('1.1.3');
} else if (args[0] === 'account' && args[1] === 'info') {
  const needsLogin = scenario === 'login-required' || scenario === 'login-cancel' || scenario === 'non-interactive';
  if (needsLogin && !(await stateExists())) process.exit(2);
  output({ user: { id: 'fake' }, activeAccount: { accountType: 'personal' }, teamId: null, accountsCount: 1 });
} else if (args[0] === 'login' && args[1] === 'web') {
  if (scenario === 'login-cancel') process.exit(130);
  if (testConfig.loginPrompt) output('fake browser login prompt');
  if (statePath) await writeFile(statePath, 'authenticated\n');
} else if (args[0] === 'project') {
  if (scenario === 'permission-denied') process.exit(3);
  const projectUuid = args[1];
  output({
    projectUuid,
    nodes: [
      { id: 'group-1', name: 'Sources', type: 'group', position: { x: 0, y: 0 }, width: 800, height: 700 },
      { id: 'image-1', name: 'Cover', type: 'image', position: { x: 20, y: 20 }, width: 320, height: 320, parentId: 'group-1' },
      { id: 'video-empty', name: 'Pending shot', type: 'video', position: { x: 400, y: 20 }, width: 320, height: 320, parentId: 'group-1' },
      { id: 'video-1', name: 'Shot', type: 'video', position: { x: 900, y: 20 }, width: 622, height: 350 },
      { id: 'audio-1', name: 'Voice', type: 'audio', position: { x: 900, y: 420 }, width: 350, height: 148 },
    ],
    edges: [{ id: 'edge-1', source: 'image-1', target: 'video-1' }],
  });
} else if (args[0] === 'node' || args[0] === 'group') {
  const nodeId = args[1];
  const details = {
    'group-1': { nodeKey: nodeId, name: 'Sources', data: { type: 'group', childNodeIds: ['image-1', 'video-empty'] } },
    'image-1': { nodeKey: nodeId, name: 'Cover', data: { type: 'image', url: ['https://signed.example.test/cover.png?token=source-secret'], resourceMeta: { items: [{ extension: 'png', mimeType: 'image/png', width: 320, height: 320 }] } } },
    'video-empty': { nodeKey: nodeId, name: 'Pending shot', data: { type: 'video', url: [] } },
    'video-1': { nodeKey: nodeId, name: 'Shot', data: { type: 'video', url: ['https://signed.example.test/shot.mp4?signature=source-secret'], poster: 'https://signed.example.test/poster.jpg?token=source-secret', resourceMeta: { items: [{ extension: 'mp4', mimeType: 'video/mp4', durationSec: 2 }] } } },
    'audio-1': { nodeKey: nodeId, name: 'Voice', data: { type: 'audio', url: ['https://signed.example.test/voice.wav?token=source-secret'], resourceMeta: { items: [{ extension: 'wav', mimeType: 'audio/wav', durationSec: 2 }] } } },
  };
  if (!details[nodeId]) process.exit(4);
  output(details[nodeId]);
} else if (args[0] === 'download') {
  const nodeId = args[args.indexOf('-n') + 1];
  const outputDirectory = args[args.indexOf('-o') + 1];
  if (scenario === 'partial-media' && nodeId === 'audio-1') process.exit(5);
  if (scenario === 'transient-media' && nodeId === 'image-1' && !(await stateExists())) {
    if (statePath) await writeFile(statePath, 'first-download-failed\n');
    process.exit(5);
  }
  const extensions = { 'image-1': 'png', 'video-1': 'mp4', 'audio-1': 'wav' };
  if (!extensions[nodeId]) process.exit(6);
  await mkdir(outputDirectory, { recursive: true });
  await writeFile(join(outputDirectory, `${nodeId}.${extensions[nodeId]}`), `fake-${nodeId}-media\n`);
} else {
  process.exit(64);
}

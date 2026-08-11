import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  EXPORT_RESULT_SCHEMA,
  MEDIA_MANIFEST_SCHEMA,
  exportLibTVURL,
  locateLibTVCLI,
  parseLibTVCanvasURL,
  sanitizedChildEnvironment,
} from './exporter.mjs';

const fakeCLI = fileURLToPath(new URL('./testdata/fake-libtv-cli.mjs', import.meta.url));
const adapterCLI = fileURLToPath(new URL('./cli.mjs', import.meta.url));
const projectId = '0123456789abcdef0123456789abcdef';
const projectURL = `https://www.liblib.tv/canvas?spaceId=123&projectId=${projectId}`;

async function exists(path) {
  try {
    await lstat(path);
    return true;
  } catch (error) {
    if (error?.code === 'ENOENT') return false;
    throw error;
  }
}

async function fixture(scenario) {
  const root = await mkdtemp(join(tmpdir(), 'pippit-libtv-export-test-'));
  const outputDir = join(root, 'bundle');
  const configDir = join(root, 'libtv-config');
  const logPath = join(root, 'commands.ndjson');
  const statePath = join(root, 'login-state');
  await mkdir(configDir, { mode: 0o700 });
  const configure = async (overrides = {}) => writeFile(
    join(configDir, 'fake-cli-test.json'),
    `${JSON.stringify({ scenario, logPath, statePath, ...overrides })}\n`,
    { mode: 0o600 },
  );
  await configure();
  return {
    root,
    outputDir,
    logPath,
    statePath,
    configure,
    options: {
      url: projectURL,
      outputDir,
      binary: fakeCLI,
      env: {
        ...process.env,
        LIBTV_CONFIG_DIR: configDir,
        XYQ_ACCESS_KEY: 'must-never-reach-libtv',
        PIPPIT_ACCESS_KEY: 'must-never-reach-libtv',
        PIPPIT_AK: 'must-never-reach-libtv',
        THIRD_PARTY_API_KEY: 'must-never-reach-libtv',
        UNKNOWN_TOKEN: 'must-never-reach-libtv',
        SSH_AUTH_SOCK: join(root, 'must-never-reach-libtv.sock'),
      },
    },
  };
}

async function readJson(path) {
  return JSON.parse(await readFile(path, 'utf8'));
}

function testChildEnvironmentAllowlist() {
  const sanitized = sanitizedChildEnvironment({
    HOME: '/safe/home',
    Path: '/safe/bin',
    LC_ALL: 'C.UTF-8',
    LIBTV_CONFIG_DIR: '/safe/libtv-config',
    HTTP_PROXY: 'http://proxy.example:8080',
    ALL_PROXY: 'socks5://proxy.example:1080',
    HTTPS_PROXY: 'https://user:password@proxy.example:443',
    FTP_PROXY: 'ftp://proxy.example',
    THIRD_PARTY_API_KEY: 'secret',
    UNKNOWN_TOKEN: 'secret',
    MY_PASSWORD: 'secret',
    SSH_AUTH_SOCK: '/tmp/agent.sock',
    LIBTV_TOKEN: 'secret',
  });
  assert.deepEqual(sanitized, {
    HOME: '/safe/home',
    Path: '/safe/bin',
    LC_ALL: 'C.UTF-8',
    LIBTV_CONFIG_DIR: '/safe/libtv-config',
    HTTP_PROXY: 'http://proxy.example:8080',
    ALL_PROXY: 'socks5://proxy.example:1080',
  });
}

async function testAuthenticatedExport() {
  const context = await fixture('authenticated');
  try {
    const result = await exportLibTVURL(context.options);
    assert.equal(result.schema, EXPORT_RESULT_SCHEMA);
    assert.equal(result.plan_schema, 'pippit-canvas-plan/0.1');
    assert.equal(result.media_count, 3);
    assert.equal(result.node_count, 4);
    assert.equal(result.group_count, 1);
    assert.equal(result.edge_count, 1);
    assert.equal(result.degradation_count, 1);
    assert.equal(result.media.length, 3);
    for (const item of result.media) {
      assert.equal(item.local_path.startsWith(`${context.outputDir}/media/`), true);
      assert.equal(await exists(item.local_path), true);
    }

    const snapshot = await readJson(result.snapshot_path);
    const manifest = await readJson(result.media_manifest_path);
    const plan = await readJson(result.plan_path);
    assert.equal(manifest.schema, MEDIA_MANIFEST_SCHEMA);
    assert.equal(manifest.media.length, 3);
    assert.deepEqual(manifest.empty_media, [{
      source_node_id: 'video-empty',
      media_type: 'video',
      reason: 'source_has_no_media',
    }]);
    for (const item of manifest.media) {
      assert.match(item.sha256, /^[0-9a-f]{64}$/);
      assert.match(item.relative_path, /^media\/[a-z0-9-]+\.[a-z0-9]+$/);
      assert.equal(item.byte_size > 0, true);
    }
    assert.equal(plan.required_media.length, 3);
    for (const item of plan.required_media) {
      assert.match(item.local_path, /^media\//);
      assert.match(item.sha256, /^[0-9a-f]{64}$/);
      assert.equal('url' in item, false);
    }
    assert.equal(plan.nodes.find((node) => node.source_node_id === 'image-1').kind, 'image');
    assert.equal(plan.nodes.find((node) => node.source_node_id === 'video-empty').kind, 'video-placeholder');
    assert.equal(plan.degradations[0].code, 'libtv.media.empty_placeholder');
    assert.equal(snapshot.assetReferences.length, 0);

    const serialized = [result.snapshot_path, result.media_manifest_path, result.plan_path]
      .map((path) => readFile(path, 'utf8'));
    const contents = (await Promise.all(serialized)).join('\n');
    for (const forbidden of ['https://', 'source-secret', 'must-never-reach-libtv', 'PIPPIT_ACCESS_KEY']) {
      assert.equal(contents.includes(forbidden), false, `bundle leaked ${forbidden}`);
    }
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.equal(commands.some((args) => args[0] === 'login'), false);
    assert.equal(commands.some((args) => args[0] === 'group' && args[1] === 'group-1'), true);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testBrowserLogin() {
  const context = await fixture('login-required');
  try {
    const result = await exportLibTVURL(context.options);
    assert.equal(await exists(result.plan_path), true);
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.equal(commands.some((args) => args.join(' ') === 'login web --open'), true);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testTransientMediaRetry() {
  const context = await fixture('transient-media');
  try {
    const result = await exportLibTVURL(context.options);
    assert.equal(result.media_count, 3);
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.equal(commands.filter((args) => args[0] === 'download' && args[2] === 'image-1').length, 2);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testLoginDoesNotPolluteJSONStdout() {
  const context = await fixture('login-required');
  try {
    await context.configure({ loginPrompt: true });
    const result = spawnSync(process.execPath, [
      adapterCLI,
      'export',
      '--url', projectURL,
      '--output-dir', context.outputDir,
      '--libtv-cli', fakeCLI,
    ], { encoding: 'utf8', env: context.options.env });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(JSON.parse(result.stdout).schema, EXPORT_RESULT_SCHEMA);
    assert.equal(result.stdout.trim().split('\n').length, 1);
    assert.match(result.stderr, /fake browser login prompt/);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testLocateUsesVerifiedBootstrap() {
  const context = await fixture('authenticated');
  try {
    const pathHijackDirectory = join(context.root, 'path-hijack');
    const pathHijackMarker = join(pathHijackDirectory, 'executed');
    await mkdir(pathHijackDirectory, { mode: 0o700 });
    await writeFile(
      join(pathHijackDirectory, 'libtv'),
      `#!/bin/sh\nprintf executed > '${pathHijackMarker}'\nprintf '1.1.3\\n'\n`,
      { mode: 0o700 },
    );
    let calls = 0;
    const found = await locateLibTVCLI({
      env: {
        ...context.options.env,
        HOME: context.root,
        PATH: `${pathHijackDirectory}:${dirname(process.execPath)}:/usr/bin:/bin`,
      },
      bootstrap: async () => {
        calls += 1;
        return fakeCLI;
      },
    });
    assert.equal(found.version, '1.1.3');
    assert.equal(found.runner.binary, fakeCLI);
    assert.equal(calls, 1);
    assert.equal(await exists(pathHijackMarker), false);

    const homeBinaryDirectory = join(context.root, '.libtv');
    const homeHijackMarker = join(homeBinaryDirectory, 'executed');
    await mkdir(homeBinaryDirectory, { mode: 0o700 });
    await writeFile(
      join(homeBinaryDirectory, 'libtv'),
      `#!/bin/sh\nprintf executed > '${homeHijackMarker}'\nprintf '1.1.3\\n'\n`,
      { mode: 0o700 },
    );
    let homeCalls = 0;
    await locateLibTVCLI({
      env: { ...context.options.env, HOME: context.root, PATH: `${dirname(process.execPath)}:/usr/bin:/bin` },
      bootstrap: async () => {
        homeCalls += 1;
        return fakeCLI;
      },
    });
    assert.equal(homeCalls, 1);
    assert.equal(await exists(homeHijackMarker), false);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testClosedFailure(scenario, expected, extra = {}) {
  const context = await fixture(scenario);
  try {
    await assert.rejects(exportLibTVURL({ ...context.options, ...extra }), expected);
    assert.equal(await exists(context.outputDir), false);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testFailureModes() {
  await testClosedFailure('non-interactive', /authentication is required/, { nonInteractive: true });
  await testClosedFailure('login-cancel', /login was cancelled/);
  await testClosedFailure('permission-denied', /permission was denied/);
  await testClosedFailure('partial-media', /download failed/);
  assert.throws(() => parseLibTVCanvasURL('https://evil.example/canvas?projectId=0123456789abcdef0123456789abcdef'), /expected an HTTPS LibTV canvas URL/);
  assert.throws(() => parseLibTVCanvasURL('https://www.liblib.tv/canvas?projectId=bad'), /projectId/);
}

await chmod(fakeCLI, 0o755);
testChildEnvironmentAllowlist();
await testAuthenticatedExport();
await testBrowserLogin();
await testTransientMediaRetry();
await testLoginDoesNotPolluteJSONStdout();
await testLocateUsesVerifiedBootstrap();
await testFailureModes();
process.stdout.write('libtv URL exporter tests passed\n');

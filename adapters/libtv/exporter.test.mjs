import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  AUTH_RESULT_SCHEMA,
  EXPORT_RESULT_SCHEMA,
  MEDIA_MANIFEST_SCHEMA,
  exportLibTVURL,
  locateLibTVCLI,
  parseLibTVCanvasURL,
  preflightLibTVAuth,
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

async function testAuthPreflightOnlyChecksAccount() {
  const context = await fixture('login-required');
  try {
    await context.configure({ loginPrompt: true });
    const result = spawnSync(process.execPath, [
      adapterCLI,
      'auth',
      '--libtv-cli', fakeCLI,
    ], { encoding: 'utf8', env: context.options.env });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim().split('\n').length, 1);
    assert.deepEqual(JSON.parse(result.stdout), {
      schema: AUTH_RESULT_SCHEMA,
      provider: 'libtv',
      authenticated: true,
      cli_version: '1.1.3',
      login_performed: true,
    });
    assert.match(result.stderr, /fake browser login prompt/);
    assert.deepEqual(
      result.stderr.trim().split('\n').filter((line) => line.startsWith('[libtv]')),
      [
        '[libtv] 阶段：准备经过校验的 LibTV 命令行工具',
        '[libtv] 阶段：检查 LibTV 登录状态',
        '[libtv] 未检测到有效的 LibTV 登录状态，正在打开浏览器完成 OAuth 授权…',
        '[libtv] 浏览器授权已完成，正在确认 LibTV 登录状态…',
        '[libtv] LibTV 授权成功',
      ],
    );
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.deepEqual(commands, [
      ['--version'],
      ['account', 'info'],
      ['login', 'web', '--open'],
      ['account', 'info'],
    ]);
    assert.equal(
      commands.some((args) => ['project', 'node', 'group', 'download'].includes(args[0])),
      false,
    );
    assert.equal(await exists(context.outputDir), false);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testAuthenticatedPreflightSkipsLogin() {
  const context = await fixture('authenticated');
  try {
    const progress = [];
    const result = await preflightLibTVAuth({
      ...context.options,
      onProgress: (message) => progress.push(message),
    });
    assert.equal(result.authenticated, true);
    assert.equal(result.login_performed, false);
    assert.deepEqual(progress, [
      '[libtv] 阶段：准备经过校验的 LibTV 命令行工具',
      '[libtv] 阶段：检查 LibTV 登录状态',
      '[libtv] LibTV 登录状态有效',
    ]);
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.deepEqual(commands, [['--version'], ['account', 'info']]);
  } finally {
    await rm(context.root, { recursive: true, force: true });
  }
}

async function testNonInteractivePreflightStopsBeforeProject() {
  const context = await fixture('non-interactive');
  try {
    const result = spawnSync(process.execPath, [
      adapterCLI,
      'auth',
      '--libtv-cli', fakeCLI,
      '--non-interactive',
    ], { encoding: 'utf8', env: context.options.env });
    assert.equal(result.status, 1);
    assert.equal(result.stdout, '');
    assert.match(result.stderr, /需要完成 LibTV 授权/);
    const commands = (await readFile(context.logPath, 'utf8')).trim().split('\n').map(JSON.parse);
    assert.deepEqual(commands, [['--version'], ['account', 'info']]);
    assert.equal(
      commands.some((args) => ['login', 'project', 'node', 'group', 'download'].includes(args[0])),
      false,
    );
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
    assert.equal(result.stdout.includes('[libtv]'), false);
    assert.match(result.stderr, /fake browser login prompt/);
    assert.deepEqual(
      result.stderr.trim().split('\n').filter((line) => line.startsWith('[libtv]')),
      [
        '[libtv] 阶段：准备经过校验的 LibTV 命令行工具',
        '[libtv] 阶段：检查 LibTV 登录状态',
        '[libtv] 未检测到有效的 LibTV 登录状态，正在打开浏览器完成 OAuth 授权…',
        '[libtv] 浏览器授权已完成，正在确认 LibTV 登录状态…',
        '[libtv] LibTV 授权成功',
        '[libtv] 阶段：正在获取 LibTV 项目信息',
        '[libtv] 项目信息：节点 5 个，连线 1 条',
        '[libtv] 节点详情进度：已完成 1/5，剩余 4',
        '[libtv] 节点详情进度：已完成 2/5，剩余 3',
        '[libtv] 节点详情进度：已完成 3/5，剩余 2',
        '[libtv] 节点详情进度：已完成 4/5，剩余 1',
        '[libtv] 节点详情进度：已完成 5/5，剩余 0',
        '[libtv] 开始下载素材：当前 1/3，已完成 0，剩余 2',
        '[libtv] 素材下载进度：已完成 1/3，剩余 2',
        '[libtv] 开始下载素材：当前 2/3，已完成 1，剩余 1',
        '[libtv] 素材下载进度：已完成 2/3，剩余 1',
        '[libtv] 开始下载素材：当前 3/3，已完成 2，剩余 0',
        '[libtv] 素材下载进度：已完成 3/3，剩余 0',
        '[libtv] LibTV 导出完成：节点 4 个，分组 1 个，连线 1 条，素材 3 个，兼容性降级 1 项',
      ],
    );
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
  await testClosedFailure('non-interactive', /需要完成 LibTV 授权/, { nonInteractive: true });
  await testClosedFailure('login-cancel', /授权已取消/);
  await testClosedFailure('permission-denied', /当前账号拥有权限/);
  await testClosedFailure('partial-media', /连续下载 3 次仍失败/);
  assert.throws(() => parseLibTVCanvasURL('https://evil.example/canvas?projectId=0123456789abcdef0123456789abcdef'), /请输入 HTTPS 格式/);
  assert.throws(() => parseLibTVCanvasURL('https://www.liblib.tv/canvas?projectId=bad'), /projectId/);
}

await chmod(fakeCLI, 0o755);
testChildEnvironmentAllowlist();
await testAuthenticatedExport();
await testBrowserLogin();
await testAuthPreflightOnlyChecksAccount();
await testAuthenticatedPreflightSkipsLogin();
await testNonInteractivePreflightStopsBeforeProject();
await testTransientMediaRetry();
await testLoginDoesNotPolluteJSONStdout();
await testLocateUsesVerifiedBootstrap();
await testFailureModes();
process.stdout.write('libtv URL exporter tests passed\n');

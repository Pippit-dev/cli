import { createHash } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createReadStream } from 'node:fs';
import {
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readdir,
  rename,
  rm,
  stat,
  writeFile,
} from 'node:fs/promises';
import { basename, dirname, extname, join, resolve } from 'node:path';

import {
  OFFICIAL_CLI_VERSION,
  OFFICIAL_INSTALLERS,
  bootstrapLibTVCLI,
} from './bootstrap.mjs';
import { convertSnapshotToCanvasPlan } from './plan.mjs';

const EXPORT_RESULT_SCHEMA = 'pippit-libtv-export-result/0.1';
const AUTH_RESULT_SCHEMA = 'pippit-libtv-auth-result/0.1';
const MEDIA_MANIFEST_SCHEMA = 'pippit-libtv-media-manifest/0.1';
const SUPPORTED_NODE_TYPES = new Set(['group', 'image', 'video', 'audio', 'video-clip']);
const MEDIA_NODE_TYPES = new Set(['image', 'video', 'audio']);
const MAX_CAPTURE_BYTES = 32 << 20;
const MEDIA_DOWNLOAD_ATTEMPTS = 3;
const CHILD_ENV_ALLOWLIST = new Set([
  'HOME', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA', 'XDG_CONFIG_HOME', 'XDG_CACHE_HOME',
  'PATH', 'PATHEXT', 'SYSTEMROOT', 'COMSPEC', 'TMP', 'TEMP', 'TMPDIR', 'LANG', 'TZ',
  'DISPLAY', 'WAYLAND_DISPLAY', 'XDG_RUNTIME_DIR', 'DBUS_SESSION_BUS_ADDRESS',
  'SSL_CERT_FILE', 'SSL_CERT_DIR', 'LIBTV_CONFIG_DIR',
]);
const PROXY_ENV_KEYS = new Set(['HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY']);
const PROXY_PROTOCOLS = new Set(['http:', 'https:', 'socks:', 'socks4:', 'socks4a:', 'socks5:', 'socks5h:']);

class LibTVExportError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'LibTVExportError';
    this.code = code;
  }
}

function parseLibTVCanvasURL(value) {
  let url;
  try {
    url = new URL(String(value));
  } catch {
    throw new LibTVExportError('INVALID_URL', 'LibTV 画布链接无效');
  }
  if (url.protocol !== 'https:' || !['www.liblib.tv', 'liblib.tv'].includes(url.hostname) || url.pathname !== '/canvas') {
    throw new LibTVExportError('INVALID_URL', '请输入 HTTPS 格式的 LibTV 画布链接');
  }
  if (url.username || url.password || url.searchParams.getAll('projectId').length !== 1) {
    throw new LibTVExportError('INVALID_URL', 'LibTV 画布链接必须且只能包含一个 projectId，且不能包含账号凭据');
  }
  const projectId = url.searchParams.get('projectId')?.trim() ?? '';
  if (!/^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$/i.test(projectId)) {
    throw new LibTVExportError('INVALID_URL', 'LibTV projectId 必须是 32 位十六进制 ID 或 UUID');
  }
  return { projectId };
}

function sanitizedChildEnvironment(input = process.env) {
  const output = {};
  for (const [key, value] of Object.entries(input)) {
    if (value === undefined) continue;
    const upper = key.toUpperCase();
    if (CHILD_ENV_ALLOWLIST.has(upper) || upper.startsWith('LC_')) {
      output[key] = value;
      continue;
    }
    if (PROXY_ENV_KEYS.has(upper) && isSafeProxyURL(value)) output[key] = value;
  }
  return output;
}

function isSafeProxyURL(value) {
  try {
    const url = new URL(String(value));
    return PROXY_PROTOCOLS.has(url.protocol) && Boolean(url.hostname) &&
      !url.username && !url.password && !url.search && !url.hash;
  } catch {
    return false;
  }
}

function reportProgress(reporter, message) {
  if (typeof reporter !== 'function') return;
  try {
    reporter(`[libtv] ${message}`);
  } catch {
    // Progress reporting must not change export semantics.
  }
}

function createCommandRunner(binary, environment) {
  return {
    binary,
    async capture(args) {
      return runChild(binary, args, environment, false);
    },
    async interactive(args) {
      return runChild(binary, args, environment, true);
    },
  };
}

function supportsExporterVersion(version) {
  const current = String(version).split(/[+-]/, 1)[0].split('.').map(Number);
  const minimum = OFFICIAL_CLI_VERSION.split('.').map(Number);
  if (current.length !== 3 || current.some((value) => !Number.isInteger(value)) || current[0] !== minimum[0]) return false;
  for (let index = 0; index < 3; index += 1) {
    if (current[index] !== minimum[index]) return current[index] > minimum[index];
  }
  return true;
}

function runChild(binary, args, environment, interactive) {
  return new Promise((resolveResult) => {
    let stdout = '';
    let stderr = '';
    let overflow = false;
    let settled = false;
    const child = spawn(binary, args, {
      env: environment,
      shell: false,
      stdio: interactive ? ['inherit', process.stderr, process.stderr] : ['ignore', 'pipe', 'pipe'],
    });
    const finish = (result) => {
      if (settled) return;
      settled = true;
      resolveResult({ stdout, stderr, ...result });
    };
    child.on('error', (error) => finish({ exitCode: null, signal: null, error }));
    if (!interactive) {
      const collect = (target) => (chunk) => {
        const text = chunk.toString('utf8');
        if (stdout.length + stderr.length + text.length > MAX_CAPTURE_BYTES) {
          overflow = true;
          child.kill('SIGTERM');
          return;
        }
        if (target === 'stdout') stdout += text;
        else stderr += text;
      };
      child.stdout.on('data', collect('stdout'));
      child.stderr.on('data', collect('stderr'));
    }
    child.on('close', (exitCode, signal) => finish({ exitCode, signal, overflow }));
  });
}

async function locateLibTVCLI(options = {}) {
  const environment = sanitizedChildEnvironment(options.env);
  const override = options.binary ?? options.env?.LIBTV_CLI_BINARY ?? options.env?.LIBTV_CLI_PATH;
  if (override) {
    const runner = createCommandRunner(override, environment);
    const result = await runner.capture(['--version']);
    const version = result.exitCode === 0 ? result.stdout.trim().match(/\d+\.\d+\.\d+(?:[-+][\w.-]+)?/)?.[0] : undefined;
    if (version && supportsExporterVersion(version)) return { runner, version };
    throw new LibTVExportError('CLI_UNAVAILABLE', `配置的 LibTV 命令行工具不可用或版本无效：${override}`);
  }
  let bootstrapped;
  try {
    bootstrapped = await (options.bootstrap ?? bootstrapLibTVCLI)({
      env: options.env,
      environment,
      cacheRoot: options.cacheRoot,
    });
  } catch (error) {
    throw new LibTVExportError(
      'CLI_BOOTSTRAP_FAILED',
      `准备 LibTV 命令行工具失败：${error instanceof Error ? error.message : String(error)}。` +
        `官方安装信息：${OFFICIAL_INSTALLERS.shell}`,
    );
  }
  const runner = createCommandRunner(bootstrapped, environment);
  const result = await runner.capture(['--version']);
  const version = result.exitCode === 0 ? result.stdout.trim().match(/\d+\.\d+\.\d+(?:[-+][\w.-]+)?/)?.[0] : undefined;
  if (version !== OFFICIAL_CLI_VERSION) {
    throw new LibTVExportError('CLI_BOOTSTRAP_FAILED', '经过校验的 LibTV 命令行工具缓存返回了非预期版本');
  }
  return { runner, version };
}

async function ensureAuthenticated(runner, nonInteractive, onProgress) {
  const probe = await runner.capture(['account', 'info']);
  if (probe.exitCode === 0) {
    reportProgress(onProgress, 'LibTV 登录状态有效');
    return { loginPerformed: false };
  }
  if (nonInteractive) {
    throw new LibTVExportError(
      'AUTH_REQUIRED',
      '需要完成 LibTV 授权；请在交互终端中重新运行，或先执行 `libtv login web --open`',
    );
  }
  reportProgress(onProgress, '未检测到有效的 LibTV 登录状态，正在打开浏览器完成 OAuth 授权…');
  const login = await runner.interactive(['login', 'web', '--open']);
  if (login.exitCode === 130 || login.signal) {
    throw new LibTVExportError('LOGIN_CANCELLED', 'LibTV 浏览器授权已取消');
  }
  if (login.exitCode !== 0) {
    throw new LibTVExportError('LOGIN_FAILED', 'LibTV 浏览器授权未完成');
  }
  reportProgress(onProgress, '浏览器授权已完成，正在确认 LibTV 登录状态…');
  const verified = await runner.capture(['account', 'info']);
  if (verified.exitCode !== 0) {
    throw new LibTVExportError('LOGIN_FAILED', '浏览器授权后仍未获取到可用的 LibTV 登录凭据');
  }
  reportProgress(onProgress, 'LibTV 授权成功');
  return { loginPerformed: true };
}

async function prepareAuthenticatedLibTVCLI(options = {}) {
  reportProgress(options.onProgress, '阶段：准备经过校验的 LibTV 命令行工具');
  const located = await locateLibTVCLI({
    binary: options.binary,
    env: options.env ?? process.env,
    bootstrap: options.bootstrap,
    cacheRoot: options.cacheRoot,
  });
  reportProgress(options.onProgress, '阶段：检查 LibTV 登录状态');
  const authentication = await ensureAuthenticated(
    located.runner,
    Boolean(options.nonInteractive),
    options.onProgress,
  );
  return { ...located, ...authentication };
}

async function preflightLibTVAuth(options = {}) {
  const { version, loginPerformed } = await prepareAuthenticatedLibTVCLI(options);
  return {
    schema: AUTH_RESULT_SCHEMA,
    provider: 'libtv',
    authenticated: true,
    cli_version: version,
    login_performed: loginPerformed,
  };
}

function parseCommandJSON(result, commandName) {
  if (result.exitCode !== 0) {
    throw new LibTVExportError('COMMAND_FAILED', `${commandName} 执行失败（退出状态：${result.exitCode ?? '无法启动'}）`);
  }
  if (result.overflow) throw new LibTVExportError('COMMAND_OUTPUT_TOO_LARGE', `${commandName} 的输出超过安全上限`);
  try {
    return JSON.parse(result.stdout);
  } catch {
    throw new LibTVExportError('INVALID_CLI_JSON', `${commandName} 未返回有效的 JSON`);
  }
}

function validateProject(project, projectId) {
  if (project?.projectUuid !== projectId || !Array.isArray(project?.nodes) || !Array.isArray(project?.edges)) {
    throw new LibTVExportError('INVALID_PROJECT', 'LibTV 项目信息不完整，或与画布链接不匹配');
  }
  const seen = new Set();
  for (const [index, node] of project.nodes.entries()) {
    if (typeof node?.id !== 'string' || !node.id.trim() || seen.has(node.id)) {
      throw new LibTVExportError('INVALID_PROJECT', `LibTV 项目中的第 ${index + 1} 个节点缺少 ID 或 ID 重复`);
    }
    if (!SUPPORTED_NODE_TYPES.has(node.type)) {
      throw new LibTVExportError('UNSUPPORTED_NODE', `暂不支持该 LibTV 节点类型：${node.type ?? '未提供类型'}`);
    }
    seen.add(node.id);
  }
}

function hasMediaResult(detail) {
  const value = detail?.data?.url;
  if (Array.isArray(value)) return value.some((item) => typeof item === 'string' && item.trim());
  return typeof value === 'string' && Boolean(value.trim());
}

function sanitizedExternalValue(value, key = '') {
  if (/url|uri|token|cookie|authorization|credential|signature|secret|access.?key/i.test(key)) return undefined;
  if (typeof value === 'string') {
    if (/\b(?:https?|data|blob):/i.test(value)) return undefined;
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizedExternalValue(item)).filter((item) => item !== undefined);
  }
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.entries(value)
      .map(([childKey, child]) => [childKey, sanitizedExternalValue(child, childKey)])
      .filter(([, child]) => child !== undefined),
  );
}

function safeFileName(value, fallback) {
  const normalized = String(value || '').replaceAll('\\', '/');
  const raw = basename(normalized).normalize('NFC').replaceAll(/[\u0000-\u001f\u007f/:*?"<>|]/g, '_');
  const characters = Array.from(raw);
  const safe = characters.slice(0, 160).join('').replace(/^\.+$/, '');
  return safe || fallback;
}

async function regularFilesUnder(root, current = root) {
  const files = [];
  for (const entry of await readdir(current, { withFileTypes: true })) {
    const path = join(current, entry.name);
    if (entry.isSymbolicLink()) throw new LibTVExportError('UNSAFE_DOWNLOAD', 'LibTV 下载结果中包含不安全的符号链接');
    if (entry.isDirectory()) files.push(...await regularFilesUnder(root, path));
    else if (entry.isFile()) files.push(path);
    else throw new LibTVExportError('UNSAFE_DOWNLOAD', 'LibTV 下载结果中包含不支持的文件类型');
  }
  return files;
}

async function fileSHA256(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}

async function writePrivateJSON(path, value) {
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  await chmod(path, 0o600);
}

async function pathExists(path) {
  try {
    await lstat(path);
    return true;
  } catch (error) {
    if (error?.code === 'ENOENT') return false;
    throw error;
  }
}

async function exportMedia(runner, tasks, projectId, stagingPath, onProgress) {
  const mediaDirectory = join(stagingPath, 'media');
  const downloadsDirectory = join(stagingPath, '.downloads');
  await mkdir(mediaDirectory, { mode: 0o700 });
  await mkdir(downloadsDirectory, { mode: 0o700 });
  const manifest = [];
  const deduplicated = new Map();
  if (tasks.length === 0) {
    reportProgress(onProgress, '素材下载进度：已完成 0/0，剩余 0');
  }
  for (const [index, task] of tasks.entries()) {
    reportProgress(
      onProgress,
      `开始下载素材：当前 ${index + 1}/${tasks.length}，已完成 ${index}，` +
        `剩余 ${tasks.length - index - 1}`,
    );
    let downloaded;
    let lastFailure = '命令执行失败';
    for (let attempt = 0; attempt < MEDIA_DOWNLOAD_ATTEMPTS; attempt += 1) {
      const downloadDirectory = join(
        downloadsDirectory,
        `${String(index).padStart(4, '0')}-attempt-${attempt + 1}`,
      );
      await mkdir(downloadDirectory, { mode: 0o700 });
      const result = await runner.capture(['download', '-n', task.node.id, '-p', projectId, '-o', downloadDirectory]);
      if (result.exitCode === 0) {
        const files = await regularFilesUnder(downloadDirectory);
        if (files.length === 1 && extname(files[0]).toLowerCase() !== '.zip') {
          const fileInfo = await stat(files[0]);
          if (fileInfo.isFile() && fileInfo.size > 0 && Number.isSafeInteger(fileInfo.size)) {
            downloaded = { path: files[0], fileInfo };
            break;
          }
          lastFailure = '生成了无效的素材文件';
        } else {
          lastFailure = '没有生成唯一且可直接使用的素材文件';
        }
      } else {
        lastFailure = `退出状态为 ${result.exitCode ?? '无法启动'}`;
      }
      if (attempt + 1 < MEDIA_DOWNLOAD_ATTEMPTS) {
        await new Promise((resolveDelay) => setTimeout(resolveDelay, 200 * (2 ** attempt)));
      }
    }
    if (!downloaded) {
      throw new LibTVExportError(
        'MEDIA_DOWNLOAD_FAILED',
        `LibTV 节点 ${task.node.id} 的 ${task.node.type} 素材连续下载 ${MEDIA_DOWNLOAD_ATTEMPTS} 次仍失败（${lastFailure}）`,
      );
    }
    const digest = await fileSHA256(downloaded.path);
    let stored = deduplicated.get(digest);
    if (!stored) {
      const originalName = safeFileName(downloaded.path, `${task.node.type}.bin`);
      const nodePrefix = createHash('sha256').update(task.node.id).digest('hex').slice(0, 12);
      const relativePath = `media/${nodePrefix}-${originalName}`;
      await rename(downloaded.path, join(stagingPath, relativePath));
      await chmod(join(stagingPath, relativePath), 0o600);
      stored = { relativePath, fileName: originalName, byteSize: downloaded.fileInfo.size };
      deduplicated.set(digest, stored);
    }
    manifest.push({
      logical_id: `media:${task.node.id}`,
      source_node_id: task.node.id,
      file_name: stored.fileName,
      media_type: task.node.type,
      relative_path: stored.relativePath,
      sha256: digest,
      byte_size: stored.byteSize,
    });
    reportProgress(
      onProgress,
      `素材下载进度：已完成 ${index + 1}/${tasks.length}，剩余 ${tasks.length - index - 1}`,
    );
  }
  await rm(downloadsDirectory, { recursive: true, force: true });
  return manifest;
}

async function exportLibTVURL(options) {
  const { projectId } = parseLibTVCanvasURL(options.url);
  const outputPath = resolve(options.outputDir);
  if (await pathExists(outputPath)) {
    throw new LibTVExportError('OUTPUT_EXISTS', `导出目录已存在：${outputPath}`);
  }
  const { runner, version } = await prepareAuthenticatedLibTVCLI({
    binary: options.binary,
    env: options.env ?? process.env,
    bootstrap: options.bootstrap,
    cacheRoot: options.cacheRoot,
    nonInteractive: options.nonInteractive,
    onProgress: options.onProgress,
  });
  reportProgress(options.onProgress, '阶段：正在获取 LibTV 项目信息');
  const projectResult = await runner.capture(['project', projectId]);
  if (projectResult.exitCode !== 0) {
    throw new LibTVExportError('PROJECT_FORBIDDEN', '无法访问该 LibTV 项目，请确认项目存在且当前账号拥有权限');
  }
  const project = parseCommandJSON(projectResult, 'LibTV 项目查询命令');
  validateProject(project, projectId);
  reportProgress(
    options.onProgress,
    `项目信息：节点 ${project.nodes.length} 个，连线 ${project.edges.length} 条`,
  );

  const nodeDetails = [];
  const mediaTasks = [];
  const emptyMedia = [];
  for (const [index, node] of project.nodes.entries()) {
    const detailCommand = node.type === 'group' ? 'group' : 'node';
    const detail = parseCommandJSON(
      await runner.capture([detailCommand, node.id, '-p', projectId]),
      `LibTV 节点详情查询命令（${node.id}）`,
    );
    const downloadable = MEDIA_NODE_TYPES.has(node.type) && hasMediaResult(detail);
    if (downloadable) mediaTasks.push({ node, detail });
    else if (node.type === 'image' || node.type === 'video') {
      emptyMedia.push({ source_node_id: node.id, media_type: node.type, reason: 'source_has_no_media' });
    } else if (node.type === 'audio') {
      throw new LibTVExportError('EMPTY_AUDIO', `LibTV 音频节点 ${node.id} 没有可下载的素材`);
    }
    nodeDetails.push({
      sourceNodeId: node.id,
      detail: sanitizedExternalValue(detail) ?? {},
      summary: { type: node.type, hasDownloadableMedia: downloadable },
    });
    reportProgress(
      options.onProgress,
      `节点详情进度：已完成 ${index + 1}/${project.nodes.length}，剩余 ${project.nodes.length - index - 1}`,
    );
  }

  await mkdir(dirname(outputPath), { recursive: true, mode: 0o700 });
  const stagingPath = await mkdtemp(join(dirname(outputPath), `.${basename(outputPath)}.staging-`));
  await chmod(stagingPath, 0o700);
  let completed = false;
  try {
    const media = await exportMedia(runner, mediaTasks, projectId, stagingPath, options.onProgress);
    const source = { platform: 'libtv', cliVersion: version, projectId };
    const snapshot = {
      protocolVersion: 'xyq-libtv-snapshot/0.1',
      exportedAt: new Date().toISOString(),
      source,
      project: sanitizedExternalValue(project),
      nodeDetails,
      assetReferences: [],
      diagnostics: { nodeDetailsSucceeded: nodeDetails.length, nodeDetailsFailed: 0 },
      stats: { nodes: project.nodes.length, edges: project.edges.length, media: media.length, emptyMedia: emptyMedia.length },
    };
    const mediaManifest = {
      schema: MEDIA_MANIFEST_SCHEMA,
      source: { provider: 'libtv', project_id: projectId, cli_version: version },
      media,
      empty_media: emptyMedia,
    };
    const plan = convertSnapshotToCanvasPlan(snapshot, { mediaManifest, title: options.title });
    const serialized = JSON.stringify({ snapshot, mediaManifest, plan });
    if (/\b(?:https?|data|blob):/i.test(serialized)) {
      throw new LibTVExportError('SANITIZATION_FAILED', '清理后的 LibTV 导出数据中仍包含外部链接，已停止导出');
    }
    await writePrivateJSON(join(stagingPath, 'snapshot.json'), snapshot);
    await writePrivateJSON(join(stagingPath, 'media-manifest.json'), mediaManifest);
    await writePrivateJSON(join(stagingPath, 'plan.json'), plan);
    await rename(stagingPath, outputPath);
    completed = true;
    reportProgress(
      options.onProgress,
      `LibTV 导出完成：节点 ${plan.nodes.length} 个，分组 ${plan.groups.length} 个，连线 ${plan.edges.length} 条，` +
        `素材 ${media.length} 个，兼容性降级 ${plan.degradations.length} 项`,
    );
    return {
      schema: EXPORT_RESULT_SCHEMA,
      plan_schema: plan.schema,
      bundle_dir: outputPath,
      snapshot_path: join(outputPath, 'snapshot.json'),
      media_manifest_path: join(outputPath, 'media-manifest.json'),
      plan_path: join(outputPath, 'plan.json'),
      source: plan.source,
      media: media.map((item) => ({
        logical_id: item.logical_id,
        media_type: item.media_type,
        local_path: join(outputPath, item.relative_path),
      })),
      media_count: media.length,
      node_count: plan.nodes.length,
      group_count: plan.groups.length,
      edge_count: plan.edges.length,
      degradation_count: plan.degradations.length,
    };
  } finally {
    if (!completed) await rm(stagingPath, { recursive: true, force: true });
  }
}

export {
  AUTH_RESULT_SCHEMA,
  EXPORT_RESULT_SCHEMA,
  MEDIA_MANIFEST_SCHEMA,
  LibTVExportError,
  OFFICIAL_CLI_VERSION,
  OFFICIAL_INSTALLERS,
  exportLibTVURL,
  locateLibTVCLI,
  parseLibTVCanvasURL,
  preflightLibTVAuth,
  sanitizedChildEnvironment,
};

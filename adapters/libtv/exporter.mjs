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
    throw new LibTVExportError('INVALID_URL', 'LibTV URL is invalid');
  }
  if (url.protocol !== 'https:' || !['www.liblib.tv', 'liblib.tv'].includes(url.hostname) || url.pathname !== '/canvas') {
    throw new LibTVExportError('INVALID_URL', 'expected an HTTPS LibTV canvas URL');
  }
  if (url.username || url.password || url.searchParams.getAll('projectId').length !== 1) {
    throw new LibTVExportError('INVALID_URL', 'LibTV URL must contain exactly one projectId and no credentials');
  }
  const projectId = url.searchParams.get('projectId')?.trim() ?? '';
  if (!/^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$/i.test(projectId)) {
    throw new LibTVExportError('INVALID_URL', 'LibTV projectId must be a 32-hex ID or UUID');
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
    throw new LibTVExportError('CLI_UNAVAILABLE', `configured LibTV CLI is unavailable or invalid: ${override}`);
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
      `LibTV CLI bootstrap failed: ${error instanceof Error ? error.message : String(error)}. ` +
        `Official installer metadata: ${OFFICIAL_INSTALLERS.shell}`,
    );
  }
  const runner = createCommandRunner(bootstrapped, environment);
  const result = await runner.capture(['--version']);
  const version = result.exitCode === 0 ? result.stdout.trim().match(/\d+\.\d+\.\d+(?:[-+][\w.-]+)?/)?.[0] : undefined;
  if (version !== OFFICIAL_CLI_VERSION) {
    throw new LibTVExportError('CLI_BOOTSTRAP_FAILED', 'verified LibTV CLI cache returned an unexpected version');
  }
  return { runner, version };
}

async function ensureAuthenticated(runner, nonInteractive) {
  const probe = await runner.capture(['account', 'info']);
  if (probe.exitCode === 0) return;
  if (nonInteractive) {
    throw new LibTVExportError('AUTH_REQUIRED', 'LibTV authentication is required; run `libtv login web --open` first');
  }
  const login = await runner.interactive(['login', 'web', '--open']);
  if (login.exitCode === 130 || login.signal) {
    throw new LibTVExportError('LOGIN_CANCELLED', 'LibTV browser login was cancelled');
  }
  if (login.exitCode !== 0) {
    throw new LibTVExportError('LOGIN_FAILED', 'LibTV browser login did not complete');
  }
  const verified = await runner.capture(['account', 'info']);
  if (verified.exitCode !== 0) {
    throw new LibTVExportError('LOGIN_FAILED', 'LibTV credentials were not available after browser login');
  }
}

function parseCommandJSON(result, commandName) {
  if (result.exitCode !== 0) {
    throw new LibTVExportError('COMMAND_FAILED', `${commandName} failed (exit ${result.exitCode ?? 'spawn'})`);
  }
  if (result.overflow) throw new LibTVExportError('COMMAND_OUTPUT_TOO_LARGE', `${commandName} output exceeded the safety limit`);
  try {
    return JSON.parse(result.stdout);
  } catch {
    throw new LibTVExportError('INVALID_CLI_JSON', `${commandName} did not return valid JSON`);
  }
}

function validateProject(project, projectId) {
  if (project?.projectUuid !== projectId || !Array.isArray(project?.nodes) || !Array.isArray(project?.edges)) {
    throw new LibTVExportError('INVALID_PROJECT', 'LibTV project summary is incomplete or does not match the URL');
  }
  const seen = new Set();
  for (const [index, node] of project.nodes.entries()) {
    if (typeof node?.id !== 'string' || !node.id.trim() || seen.has(node.id)) {
      throw new LibTVExportError('INVALID_PROJECT', `LibTV project node ${index} has a missing or duplicate ID`);
    }
    if (!SUPPORTED_NODE_TYPES.has(node.type)) {
      throw new LibTVExportError('UNSUPPORTED_NODE', `unsupported LibTV node type: ${node.type ?? '<missing>'}`);
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
    if (entry.isSymbolicLink()) throw new LibTVExportError('UNSAFE_DOWNLOAD', 'LibTV download produced a symbolic link');
    if (entry.isDirectory()) files.push(...await regularFilesUnder(root, path));
    else if (entry.isFile()) files.push(path);
    else throw new LibTVExportError('UNSAFE_DOWNLOAD', 'LibTV download produced an unsupported filesystem entry');
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

async function exportMedia(runner, tasks, projectId, stagingPath) {
  const mediaDirectory = join(stagingPath, 'media');
  const downloadsDirectory = join(stagingPath, '.downloads');
  await mkdir(mediaDirectory, { mode: 0o700 });
  await mkdir(downloadsDirectory, { mode: 0o700 });
  const manifest = [];
  const deduplicated = new Map();
  for (const [index, task] of tasks.entries()) {
    let downloaded;
    let lastFailure = 'command failed';
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
          lastFailure = 'produced an invalid media file';
        } else {
          lastFailure = 'did not produce one direct media file';
        }
      } else {
        lastFailure = `failed with exit ${result.exitCode ?? 'spawn'}`;
      }
      if (attempt + 1 < MEDIA_DOWNLOAD_ATTEMPTS) {
        await new Promise((resolveDelay) => setTimeout(resolveDelay, 200 * (2 ** attempt)));
      }
    }
    if (!downloaded) {
      throw new LibTVExportError(
        'MEDIA_DOWNLOAD_FAILED',
        `LibTV ${task.node.type} download failed for node ${task.node.id} after ${MEDIA_DOWNLOAD_ATTEMPTS} attempts (${lastFailure})`,
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
  }
  await rm(downloadsDirectory, { recursive: true, force: true });
  return manifest;
}

async function exportLibTVURL(options) {
  const { projectId } = parseLibTVCanvasURL(options.url);
  const outputPath = resolve(options.outputDir);
  if (await pathExists(outputPath)) {
    throw new LibTVExportError('OUTPUT_EXISTS', `output directory already exists: ${outputPath}`);
  }
  const { runner, version } = await locateLibTVCLI({
    binary: options.binary,
    env: options.env ?? process.env,
    bootstrap: options.bootstrap,
    cacheRoot: options.cacheRoot,
  });
  await ensureAuthenticated(runner, Boolean(options.nonInteractive));
  const projectResult = await runner.capture(['project', projectId]);
  if (projectResult.exitCode !== 0) {
    throw new LibTVExportError('PROJECT_FORBIDDEN', 'LibTV project is unavailable or permission was denied');
  }
  const project = parseCommandJSON(projectResult, 'libtv project');
  validateProject(project, projectId);

  const nodeDetails = [];
  const mediaTasks = [];
  const emptyMedia = [];
  for (const node of project.nodes) {
    const detailCommand = node.type === 'group' ? 'group' : 'node';
    const detail = parseCommandJSON(
      await runner.capture([detailCommand, node.id, '-p', projectId]),
      `libtv ${detailCommand} ${node.id}`,
    );
    const downloadable = MEDIA_NODE_TYPES.has(node.type) && hasMediaResult(detail);
    if (downloadable) mediaTasks.push({ node, detail });
    else if (node.type === 'image' || node.type === 'video') {
      emptyMedia.push({ source_node_id: node.id, media_type: node.type, reason: 'source_has_no_media' });
    } else if (node.type === 'audio') {
      throw new LibTVExportError('EMPTY_AUDIO', `LibTV audio node ${node.id} has no downloadable media`);
    }
    nodeDetails.push({
      sourceNodeId: node.id,
      detail: sanitizedExternalValue(detail) ?? {},
      summary: { type: node.type, hasDownloadableMedia: downloadable },
    });
  }

  await mkdir(dirname(outputPath), { recursive: true, mode: 0o700 });
  const stagingPath = await mkdtemp(join(dirname(outputPath), `.${basename(outputPath)}.staging-`));
  await chmod(stagingPath, 0o700);
  let completed = false;
  try {
    const media = await exportMedia(runner, mediaTasks, projectId, stagingPath);
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
      throw new LibTVExportError('SANITIZATION_FAILED', 'sanitized LibTV bundle still contains an external URL');
    }
    await writePrivateJSON(join(stagingPath, 'snapshot.json'), snapshot);
    await writePrivateJSON(join(stagingPath, 'media-manifest.json'), mediaManifest);
    await writePrivateJSON(join(stagingPath, 'plan.json'), plan);
    await rename(stagingPath, outputPath);
    completed = true;
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
  EXPORT_RESULT_SCHEMA,
  MEDIA_MANIFEST_SCHEMA,
  LibTVExportError,
  OFFICIAL_CLI_VERSION,
  OFFICIAL_INSTALLERS,
  exportLibTVURL,
  locateLibTVCLI,
  parseLibTVCanvasURL,
  sanitizedChildEnvironment,
};

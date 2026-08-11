import { createHash } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createReadStream, createWriteStream } from 'node:fs';
import {
  chmod,
  copyFile,
  lstat,
  mkdir,
  mkdtemp,
  readdir,
  rename,
  rm,
  writeFile,
} from 'node:fs/promises';
import { get as httpsGet } from 'node:https';
import { homedir, tmpdir } from 'node:os';
import { basename, join, parse, resolve } from 'node:path';
import { Transform } from 'node:stream';
import { pipeline } from 'node:stream/promises';

const OFFICIAL_CLI_VERSION = '1.1.3';
const OFFICIAL_CLI_ORIGIN = 'https://liblibai-web-static.liblib.cloud';
const OFFICIAL_INSTALLERS = Object.freeze({
  shell: `${OFFICIAL_CLI_ORIGIN}/cli/1.1.3/install-libtv-cli.sh`,
  powershell: `${OFFICIAL_CLI_ORIGIN}/cli/1.1.3/install-libtv-cli.ps1`,
  cmd: `${OFFICIAL_CLI_ORIGIN}/cli/1.1.3/install-libtv-cli.bat`,
});
// Activity 240 version 1.1.3 artifacts, independently downloaded and hashed on 2026-08-11.
const OFFICIAL_ARTIFACTS = Object.freeze({
  'darwin-arm64': Object.freeze({
    zipName: 'libtv-macos-arm64.zip',
    zipSHA256: '95c21012530917da8ce69eb01ebb197f418783904a0dcd16ddcf27efe5139df7',
    binarySHA256: '1abc924df7fb3d3428b890909c78241749136797d3c046c0b810653e9fbf6fdd',
    executable: 'libtv',
  }),
  'darwin-x64': Object.freeze({
    zipName: 'libtv-macos-x64.zip',
    zipSHA256: 'e8dfad868919522cd1e3ea5f437506bdd193684b96cf3db89512130abddb6347',
    binarySHA256: '0248dd94bdbee377f67153883110f6d48e2c1bebeeeb69d1673c30e257c84592',
    executable: 'libtv',
  }),
  'linux-arm64': Object.freeze({
    zipName: 'libtv-linux-arm64.zip',
    zipSHA256: '369b43f5be1d28dbbde7c1b6711ed746bf9bff1028ba794a6dae4fa01bed601c',
    binarySHA256: '1fe47f1d3b56f826e72c4d4a9b452a1538f5d6974d7ee319678685387d14f43f',
    executable: 'libtv',
  }),
  'linux-x64': Object.freeze({
    zipName: 'libtv-linux-x64.zip',
    zipSHA256: 'cf86f462c5aed60f95dca978cc91ece98c60bcfa27337da008ad59953c3ea7da',
    binarySHA256: 'e79ad52170556b44e957174f880c8a69057f668ddd0b9a4011524cac072c31f3',
    executable: 'libtv',
  }),
  'win32-arm64': Object.freeze({
    zipName: 'libtv-windows-arm64.zip',
    zipSHA256: 'a64f14987ba44cf7345451d557a4dfe527db7b343a1c7032ecf23bcc320974ee',
    binarySHA256: '3ccff728c39277d8ad596d8a3b24bbc071579f83f6bc102c4051233cab2734bc',
    executable: 'libtv.exe',
  }),
  'win32-x64': Object.freeze({
    // The official PowerShell installer names this remote architecture "amd64".
    zipName: 'libtv-windows-amd64.zip',
    zipSHA256: '5c5e14b683ebbafba4b2c156be305384bd9f558169d019442e53b2ea04206bd5',
    binarySHA256: 'a607ea1f557cb513f302138e64d86312b9dfa9e7eed9dffc5c07f5559192f3fb',
    executable: 'libtv.exe',
  }),
});
const MAX_ZIP_BYTES = 256 << 20;

class LibTVBootstrapError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'LibTVBootstrapError';
    this.code = code;
  }
}

function artifactFor(platform = process.platform, arch = process.arch, artifacts = OFFICIAL_ARTIFACTS) {
  const key = `${platform}-${arch}`;
  const artifact = artifacts[key];
  if (!artifact) throw new LibTVBootstrapError('UNSUPPORTED_PLATFORM', `LibTV CLI bootstrap does not support ${key}`);
  return {
    ...artifact,
    key,
    url: `${OFFICIAL_CLI_ORIGIN}/cli/${OFFICIAL_CLI_VERSION}/${artifact.zipName}`,
  };
}

function defaultCacheRoot(environment = process.env, platform = process.platform) {
  if (environment.PIPPIT_CLI_LIBTV_CACHE_DIR) return resolve(environment.PIPPIT_CLI_LIBTV_CACHE_DIR);
  const home = environment.HOME || environment.USERPROFILE || homedir();
  let base;
  if (platform === 'darwin') base = join(home, 'Library', 'Caches');
  else if (platform === 'win32') base = environment.LOCALAPPDATA || join(home, 'AppData', 'Local');
  else base = environment.XDG_CACHE_HOME || join(home, '.cache');
  return join(base, 'pippit-cli', 'tools', 'libtv');
}

function assertSafeCacheRoot(cacheRoot, environment, platform) {
  const home = resolve(environment.HOME || environment.USERPROFILE || homedir());
  const unsafe = new Set([parse(cacheRoot).root, home, resolve(tmpdir())]);
  if (platform === 'darwin') unsafe.add(join(home, 'Library', 'Caches'));
  else if (platform === 'win32') unsafe.add(resolve(environment.LOCALAPPDATA || join(home, 'AppData', 'Local')));
  else unsafe.add(resolve(environment.XDG_CACHE_HOME || join(home, '.cache')));
  if (unsafe.has(cacheRoot)) {
    throw new LibTVBootstrapError('UNSAFE_CACHE_ROOT', 'refusing to use a broad directory as the LibTV CLI cache root');
  }
}

async function fileSHA256(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}

async function verifiedCachedBinary(platformDirectory, artifact) {
  const binaryPath = join(platformDirectory, artifact.executable);
  let info;
  try {
    info = await lstat(binaryPath);
  } catch (error) {
    if (error?.code === 'ENOENT') {
      if (await pathExists(platformDirectory)) {
        throw new LibTVBootstrapError('CACHE_INTEGRITY_FAILED', 'LibTV CLI cache directory is incomplete');
      }
      return undefined;
    }
    throw error;
  }
  if (!info.isFile() || info.isSymbolicLink()) {
    throw new LibTVBootstrapError('CACHE_INTEGRITY_FAILED', 'cached LibTV CLI is not a regular file');
  }
  const digest = await fileSHA256(binaryPath);
  if (digest !== artifact.binarySHA256) {
    throw new LibTVBootstrapError('CACHE_INTEGRITY_FAILED', 'cached LibTV CLI failed SHA-256 verification');
  }
  await chmod(binaryPath, 0o700);
  return binaryPath;
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

function httpsResponse(url) {
  return new Promise((resolveResponse, rejectResponse) => {
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:' || parsed.origin !== OFFICIAL_CLI_ORIGIN) {
      rejectResponse(new LibTVBootstrapError('UNTRUSTED_DOWNLOAD', 'refusing a non-official LibTV CLI URL'));
      return;
    }
    const request = httpsGet(parsed, { headers: { 'user-agent': 'pippit-cli-libtv-bootstrap/0.1' } }, resolveResponse);
    request.setTimeout(30_000, () => request.destroy(new Error('download timed out')));
    request.on('error', rejectResponse);
  });
}

async function downloadOfficialZip({ url, destination }) {
  const response = await httpsResponse(url);
  if (response.statusCode !== 200) {
    response.resume();
    throw new LibTVBootstrapError('DOWNLOAD_FAILED', `official LibTV CLI download returned HTTP ${response.statusCode}`);
  }
  const declared = Number(response.headers['content-length']);
  if (Number.isFinite(declared) && declared > MAX_ZIP_BYTES) {
    response.destroy();
    throw new LibTVBootstrapError('DOWNLOAD_TOO_LARGE', 'official LibTV CLI ZIP exceeds the size limit');
  }
  let received = 0;
  const limiter = new Transform({
    transform(chunk, _encoding, callback) {
      received += chunk.length;
      if (received > MAX_ZIP_BYTES) callback(new LibTVBootstrapError('DOWNLOAD_TOO_LARGE', 'official LibTV CLI ZIP exceeds the size limit'));
      else callback(null, chunk);
    },
  });
  await pipeline(response, limiter, createWriteStream(destination, { mode: 0o600 }));
  await chmod(destination, 0o600);
}

function runTool(command, args, environment) {
  return new Promise((resolveResult) => {
    let stdout = '';
    let stderr = '';
    let settled = false;
    const child = spawn(command, args, { env: environment, shell: false, stdio: ['ignore', 'pipe', 'pipe'] });
    const finish = (result) => {
      if (settled) return;
      settled = true;
      resolveResult({ stdout, stderr, ...result });
    };
    child.on('error', (error) => finish({ exitCode: null, error }));
    child.stdout.on('data', (chunk) => { if (stdout.length < (4 << 20)) stdout += chunk.toString('utf8'); });
    child.stderr.on('data', (chunk) => { if (stderr.length < (4 << 20)) stderr += chunk.toString('utf8'); });
    child.on('close', (exitCode) => finish({ exitCode }));
  });
}

function validateArchiveListing(listing) {
  const entries = listing.split(/\r?\n/).filter(Boolean);
  if (entries.length === 0) throw new LibTVBootstrapError('INVALID_ARCHIVE', 'official LibTV CLI ZIP is empty');
  for (const entry of entries) {
    const parts = entry.split('/').filter(Boolean);
    if (entry.includes('\\') || entry.startsWith('/') || /^[A-Za-z]:/.test(entry) || parts.includes('..')) {
      throw new LibTVBootstrapError('UNSAFE_ARCHIVE', 'official LibTV CLI ZIP contains an unsafe path');
    }
  }
}

async function extractOfficialZip({ zipPath, destination, platform, environment }) {
  const command = platform === 'win32' ? 'tar.exe' : 'unzip';
  const listArgs = platform === 'win32' ? ['-tf', zipPath] : ['-Z1', zipPath];
  const listing = await runTool(command, listArgs, environment);
  if (listing.exitCode !== 0) {
    const hint = platform === 'win32' ? 'Windows 10+ built-in tar.exe is required' : 'unzip is required';
    throw new LibTVBootstrapError('EXTRACTOR_UNAVAILABLE', `cannot inspect official LibTV CLI ZIP; ${hint}`);
  }
  validateArchiveListing(listing.stdout);
  const extractArgs = platform === 'win32'
    ? ['-xf', zipPath, '-C', destination]
    : ['-q', zipPath, '-d', destination];
  const extracted = await runTool(command, extractArgs, environment);
  if (extracted.exitCode !== 0) {
    throw new LibTVBootstrapError('EXTRACT_FAILED', 'failed to extract verified official LibTV CLI ZIP');
  }
}

async function findBinary(root, executable, current = root) {
  const candidates = [];
  for (const entry of await readdir(current, { withFileTypes: true })) {
    const path = join(current, entry.name);
    if (entry.isSymbolicLink()) throw new LibTVBootstrapError('UNSAFE_ARCHIVE', 'official LibTV CLI ZIP contains a symbolic link');
    if (entry.isDirectory()) candidates.push(...await findBinary(root, executable, path));
    else if (entry.isFile() && basename(path) === executable) candidates.push(path);
  }
  return candidates;
}

async function writeMetadata(path, artifact) {
  const metadata = {
    schema: 'pippit-libtv-tool-cache/0.1',
    version: OFFICIAL_CLI_VERSION,
    platform: artifact.key,
    source: artifact.url,
    zip_sha256: artifact.zipSHA256,
    binary_sha256: artifact.binarySHA256,
  };
  await writeFile(path, `${JSON.stringify(metadata, null, 2)}\n`, { mode: 0o600 });
  await chmod(path, 0o600);
}

async function bootstrapLibTVCLI(options = {}) {
  const platform = options.platform ?? process.platform;
  const arch = options.arch ?? process.arch;
  const artifact = artifactFor(platform, arch, options.artifacts);
  const sourceEnvironment = options.env ?? process.env;
  const environment = options.environment ?? sourceEnvironment;
  const cacheRoot = resolve(options.cacheRoot ?? defaultCacheRoot(sourceEnvironment, platform));
  assertSafeCacheRoot(cacheRoot, sourceEnvironment, platform);
  const versionDirectory = join(cacheRoot, OFFICIAL_CLI_VERSION);
  const platformDirectory = join(versionDirectory, artifact.key);
  const cached = await verifiedCachedBinary(platformDirectory, artifact);
  if (cached) return cached;

  await mkdir(versionDirectory, { recursive: true, mode: 0o700 });
  await chmod(cacheRoot, 0o700);
  await chmod(versionDirectory, 0o700);
  const staging = await mkdtemp(join(versionDirectory, '.bootstrap-'));
  await chmod(staging, 0o700);
  try {
    const zipPath = join(staging, artifact.zipName);
    await (options.download ?? downloadOfficialZip)({ url: artifact.url, destination: zipPath, artifact });
    if (await fileSHA256(zipPath) !== artifact.zipSHA256) {
      throw new LibTVBootstrapError('ZIP_INTEGRITY_FAILED', 'official LibTV CLI ZIP failed SHA-256 verification');
    }
    const extractionDirectory = join(staging, 'extracted');
    await mkdir(extractionDirectory, { mode: 0o700 });
    await (options.extract ?? extractOfficialZip)({
      zipPath,
      destination: extractionDirectory,
      platform,
      environment,
      artifact,
    });
    const binaries = await findBinary(extractionDirectory, artifact.executable);
    if (binaries.length !== 1) {
      throw new LibTVBootstrapError('INVALID_ARCHIVE', 'verified official LibTV CLI ZIP must contain exactly one binary');
    }
    if (await fileSHA256(binaries[0]) !== artifact.binarySHA256) {
      throw new LibTVBootstrapError('BINARY_INTEGRITY_FAILED', 'official LibTV CLI binary failed SHA-256 verification');
    }
    const installed = join(staging, 'installed');
    await mkdir(installed, { mode: 0o700 });
    const installedBinary = join(installed, artifact.executable);
    await copyFile(binaries[0], installedBinary);
    await chmod(installedBinary, 0o700);
    await writeMetadata(join(installed, 'metadata.json'), artifact);
    try {
      await rename(installed, platformDirectory);
    } catch (error) {
      if (!['EEXIST', 'ENOTEMPTY'].includes(error?.code)) throw error;
    }
    const verified = await verifiedCachedBinary(platformDirectory, artifact);
    if (!verified) throw new LibTVBootstrapError('CACHE_INSTALL_FAILED', 'verified LibTV CLI cache install did not complete');
    return verified;
  } finally {
    await rm(staging, { recursive: true, force: true });
  }
}

export {
  LibTVBootstrapError,
  OFFICIAL_ARTIFACTS,
  OFFICIAL_CLI_VERSION,
  OFFICIAL_INSTALLERS,
  artifactFor,
  bootstrapLibTVCLI,
  defaultCacheRoot,
};

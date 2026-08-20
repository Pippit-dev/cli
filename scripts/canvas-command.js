"use strict";

const fs = require("fs");
const http = require("http");
const https = require("https");
const os = require("os");
const path = require("path");
const { spawn } = require("child_process");
const { createHash, randomBytes } = require("crypto");
const { pathToFileURL } = require("url");

const DEFAULT_SDK_MODULE = path.join(
  __dirname,
  "..",
  "dist",
  "xyq-canvas-command-runtime.cjs"
);
const MAX_INPUT_BYTES = 64 * 1024 * 1024;
const PERSISTENCE_VERSION = 1;
const OPTIONAL_SCHEMA = Symbol("optional canvas command schema");
const CHECKPOINT_COMMANDS = new Set([
  "compare_checkpoint",
  "create_checkpoint",
  "list_checkpoints",
  "restore_checkpoint",
]);
const CANVAS_COMMAND_PERMISSIONS = [
  "canvas.read",
  "canvas.write",
  "canvas.patch",
  "canvas.asset.read",
  "canvas.asset.write",
  "canvas.checkpoint.create",
  "canvas.checkpoint.restore",
  "canvas.permission.read",
];

const COMMAND_HELP = `用法:
  pippit-tool-cli canvas command list
  pippit-tool-cli canvas command describe <command>
  pippit-tool-cli canvas command run <command> --canvas-id <id> [--input <JSON> | --file <path|->]
`;

function isCanvasCommand(args) {
  return args[0] === "canvas" && args[1] === "command";
}

function parseCanvasCommandArgs(args) {
  if (!isCanvasCommand(args)) throw new Error("仅支持 canvas command 命令");
  const values = args.slice(2);
  if (!values.length || values[0] === "--help" || values[0] === "-h") {
    return { action: "help" };
  }

  const action = values[0];
  if (!new Set(["list", "describe", "run"]).has(action)) {
    throw new Error(`未知的 canvas command 子命令：${action}`);
  }
  let canvasId = "";
  let commandName = "";
  let filePath = "";
  let input = "";
  for (let index = 1; index < values.length; index += 1) {
    const value = values[index];
    if (value === "--help" || value === "-h") return { action: "help" };
    if (value === "--canvas-id" || value === "--file" || value === "--input") {
      const next = values[index + 1];
      if (next === undefined || next.startsWith("--")) {
        throw new Error(`参数 ${value} 缺少取值`);
      }
      if (value === "--canvas-id") canvasId = next.trim();
      if (value === "--file") filePath = next;
      if (value === "--input") input = next;
      index += 1;
      continue;
    }
    if (value.startsWith("--canvas-id=")) {
      canvasId = value.slice("--canvas-id=".length).trim();
      continue;
    }
    if (value.startsWith("--file=")) {
      filePath = value.slice("--file=".length);
      continue;
    }
    if (value.startsWith("--input=")) {
      input = value.slice("--input=".length);
      continue;
    }
    if (value.startsWith("-")) throw new Error(`未知参数：${value}`);
    if (commandName) throw new Error(`多余的位置参数：${value}`);
    commandName = value;
  }

  if (action === "list") {
    if (commandName || canvasId || filePath || input) throw new Error("canvas command list 不接受额外参数");
    return { action };
  }
  if (!commandName) throw new Error(`canvas command ${action} 缺少 command 名称`);
  if (action === "describe") {
    if (canvasId || filePath || input) throw new Error("canvas command describe 不接受运行参数");
    return { action, commandName };
  }
  if (!canvasId) throw new Error("canvas command run 缺少必填参数 --canvas-id");
  if (input && filePath) throw new Error("--input 和 --file 不能同时使用");
  return { action, canvasId, commandName, filePath, input };
}

function spawnProcess(invocation, args, options = {}) {
  return new Promise((resolve, reject) => {
    if (!invocation || !invocation.command) {
      reject(new Error("原生 CLI 执行入口缺失"));
      return;
    }
    const child = spawn(invocation.command, [...(invocation.prefixArgs || []), ...args], {
      cwd: options.cwd,
      shell: false,
      stdio: options.interactive ? "inherit" : ["pipe", "pipe", "pipe"],
      windowsHide: true,
    });
    if (options.interactive) {
      child.once("error", reject);
      child.once("close", (exitCode, signal) => resolve({ exitCode, signal }));
      return;
    }
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", reject);
    child.once("close", (exitCode, signal) => resolve({ exitCode, signal, stderr, stdout }));
    child.stdin.end(options.input);
  });
}

class NativeCommandError extends Error {
  constructor(args, result) {
    const detail = result.stderr && result.stderr.trim();
    const suffix = detail
      ? `：${detail}`
      : result.signal
        ? `（信号 ${result.signal}）`
        : `（退出码 ${result.exitCode ?? "unknown"}）`;
    super(`原生 CLI 命令执行失败：${args.join(" ")}${suffix}`);
    this.name = "NativeCommandError";
    this.exitCode = result.exitCode;
  }
}

function isAuthenticationFailure(error) {
  return error instanceof NativeCommandError &&
    (/\bHTTP\s+(401|403)\b/i.test(error.message) || /\bret\s*=\s*["']?1015["']?/i.test(error.message));
}

class NativeCanvasClient {
  constructor(invocation, options = {}) {
    this.invocation = invocation;
    this.cwd = options.cwd;
    this.stderr = options.stderr || process.stderr;
  }

  async runJSON(args, input) {
    const result = await spawnProcess(this.invocation, args, {
      cwd: this.cwd,
      input: input === undefined ? undefined : JSON.stringify(input),
    });
    if (result.exitCode !== 0) throw new NativeCommandError(args, result);
    try {
      return JSON.parse(result.stdout);
    } catch (error) {
      throw new Error(`原生 CLI 未返回有效 JSON（${args.join(" ")}）：${error.message}`);
    }
  }

  async ensureAuthenticated() {
    let status;
    try {
      status = await this.runJSON(["status"]);
    } catch (_) {
      status = undefined;
    }
    if (status?.logged_in === true) return status;
    this.stderr.write("小云雀 CLI 尚未登录，正在打开网页授权…\n");
    const result = await spawnProcess(this.invocation, ["login"], {
      cwd: this.cwd,
      interactive: true,
    });
    if (result.exitCode !== 0) throw new NativeCommandError(["login"], result);
    status = await this.runJSON(["status"]);
    if (status?.logged_in !== true) {
      throw new Error("网页授权完成后仍未检测到有效的小云雀 CLI 登录态");
    }
    return status;
  }

  async ensureCanvasAccess(canvasId) {
    let status = await this.ensureAuthenticated();
    try {
      await this.getAssets([canvasId]);
      return status;
    } catch (error) {
      if (!isAuthenticationFailure(error)) throw error;
    }
    this.stderr.write("小云雀 CLI 登录态已失效，正在重新打开网页授权…\n");
    const result = await spawnProcess(this.invocation, ["login", "--force"], {
      cwd: this.cwd,
      interactive: true,
    });
    if (result.exitCode !== 0) throw new NativeCommandError(["login", "--force"], result);
    status = await this.runJSON(["status"]);
    if (status?.logged_in !== true) throw new Error("重新授权后仍未检测到有效的小云雀 CLI 登录态");
    await this.getAssets([canvasId]);
    return status;
  }

  allocateAssetIds(count) {
    return this.runJSON(["canvas", "allocate", "--count", String(count)]).then((result) => {
      if (!Array.isArray(result.asset_ids) || result.asset_ids.length !== count) {
        throw new Error(`申请画布资产 ID 失败：期望 ${count} 个，实际返回 ${result.asset_ids?.length ?? 0} 个`);
      }
      return result.asset_ids;
    });
  }

  apply(request) {
    if (!Array.isArray(request?.transactions) || request.transactions.length !== 1) {
      throw new Error("画布同步一次只允许提交一个 transaction");
    }
    return this.runJSON(["canvas", "apply", "--transport-result", "--file", "-"], request);
  }

  async getAssets(assetIds) {
    if (!Array.isArray(assetIds) || assetIds.length === 0) return [];
    const assets = [];
    for (let offset = 0; offset < assetIds.length; offset += 50) {
      const args = ["canvas", "get"];
      for (const assetId of assetIds.slice(offset, offset + 50)) {
        args.push("--asset-id", String(assetId));
      }
      const result = await this.runJSON(args);
      if (!Array.isArray(result.assets)) throw new Error("画布资产查询结果缺少 assets");
      assets.push(...result.assets);
    }
    return assets;
  }

  async getAssetsAllowMissing(assetIds) {
    try {
      return await this.getAssets(assetIds);
    } catch (error) {
      if (!(error instanceof NativeCommandError) || !error.message.includes("did not return requested assets")) {
        throw error;
      }
    }
    const assets = [];
    for (const assetId of assetIds) {
      try {
        assets.push(...(await this.getAssets([assetId])));
      } catch (error) {
        if (!(error instanceof NativeCommandError) || !error.message.includes("did not return requested assets")) {
          throw error;
        }
      }
    }
    return assets;
  }
}

function defaultStateDirectory() {
  if (process.platform === "win32") {
    return path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local"), "pippit-tool-cli", "canvas-command");
  }
  if (process.platform === "darwin") {
    return path.join(os.homedir(), "Library", "Application Support", "pippit-tool-cli", "canvas-command");
  }
  return path.join(process.env.XDG_STATE_HOME || path.join(os.homedir(), ".local", "state"), "pippit-tool-cli", "canvas-command");
}

function emptyPersistenceState() {
  return { clientId: null, outbound: [], snapshot: null, version: PERSISTENCE_VERSION };
}

function canvasStateKey(credentialScope, canvasId) {
  return createHash("sha256")
    .update(JSON.stringify([String(credentialScope), String(canvasId)]))
    .digest("hex");
}

function validatePersistenceState(value, statePath) {
  if (
    !value || value.version !== PERSISTENCE_VERSION ||
    (value.clientId !== null && typeof value.clientId !== "string") ||
    !Array.isArray(value.outbound) ||
    (value.snapshot !== null && typeof value.snapshot !== "object")
  ) {
    throw new Error(`画布 command 本地恢复状态损坏：${statePath}`);
  }
  return value;
}

async function atomicWriteJSON(statePath, value) {
  const directory = path.dirname(statePath);
  await fs.promises.mkdir(directory, { mode: 0o700, recursive: true });
  const temporary = `${statePath}.${process.pid}.${randomBytes(8).toString("hex")}.tmp`;
  let handle;
  try {
    handle = await fs.promises.open(temporary, "wx", 0o600);
    await handle.writeFile(`${JSON.stringify(value)}\n`, "utf8");
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fs.promises.rename(temporary, statePath);
  } finally {
    await handle?.close().catch(() => {});
    await fs.promises.unlink(temporary).catch(() => {});
  }
}

function createFilePersistence({ canvasId, credentialScope, stateDirectory = defaultStateDirectory() }) {
  if (!String(credentialScope || "").trim()) throw new Error("持久化画布状态缺少 credential_scope");
  const stateKey = canvasStateKey(credentialScope, canvasId);
  const statePath = path.join(stateDirectory, `${stateKey}.json`);
  let tail = Promise.resolve();
  const enqueue = (operation) => {
    const result = tail.then(operation, operation);
    tail = result.catch(() => {});
    return result;
  };
  const load = async () => {
    try {
      return validatePersistenceState(JSON.parse(await fs.promises.readFile(statePath, "utf8")), statePath);
    } catch (error) {
      if (error?.code === "ENOENT") return emptyPersistenceState();
      if (error instanceof SyntaxError) throw new Error(`画布 command 本地恢复状态损坏：${statePath}`);
      throw error;
    }
  };
  const read = (selector) => enqueue(async () => selector(await load()));
  const update = (mutate) => enqueue(async () => {
    const state = await load();
    mutate(state);
    await atomicWriteJSON(statePath, state);
  });
  return {
    appendOutbound: (envelope) => update((state) => state.outbound.push(envelope)),
    clear: () => update((state) => Object.assign(state, emptyPersistenceState())),
    loadClientId: () => read((state) => state.clientId),
    loadOutbound: () => read((state) => state.outbound),
    loadSnapshot: () => read((state) => state.snapshot),
    removeOutbound: (txId) => update((state) => {
      state.outbound = state.outbound.filter((envelope) => envelope.txId !== txId);
    }),
    replaceOutbound: (envelopes) => update((state) => {
      state.outbound = [...envelopes];
    }),
    replaceOutboundPartition: (active, capturedTxIds) => update((state) => {
      const replaced = new Set([...capturedTxIds, ...active.map((envelope) => envelope.txId)]);
      state.outbound = [...active, ...state.outbound.filter((envelope) => !replaced.has(envelope.txId))];
    }),
    saveClientId: (clientId) => update((state) => {
      state.clientId = clientId;
    }),
    saveSnapshot: (snapshot) => update((state) => {
      state.snapshot = snapshot;
    }),
    quarantine: () => enqueue(async () => {
      const quarantinePath = `${statePath}.ambiguous.${Date.now()}.${randomBytes(6).toString("hex")}.json`;
      try {
        await fs.promises.rename(statePath, quarantinePath);
        return quarantinePath;
      } catch (error) {
        if (error?.code === "ENOENT") return "";
        throw error;
      }
    }),
    statePath,
  };
}

function createFileCheckpointStore({ canvasId, credentialScope, stateDirectory = defaultStateDirectory() }) {
  if (!String(credentialScope || "").trim()) throw new Error("持久化 checkpoint 缺少 credential_scope");
  const statePath = path.join(stateDirectory, `${canvasStateKey(credentialScope, canvasId)}.checkpoints.json`);
  let tail = Promise.resolve();
  const enqueue = (operation) => {
    const result = tail.then(operation, operation);
    tail = result.catch(() => {});
    return result;
  };
  const load = async () => {
    try {
      const value = JSON.parse(await fs.promises.readFile(statePath, "utf8"));
      if (value?.version !== PERSISTENCE_VERSION || !Array.isArray(value.checkpoints)) {
        throw new Error("invalid");
      }
      return value;
    } catch (error) {
      if (error?.code === "ENOENT") return { checkpoints: [], version: PERSISTENCE_VERSION };
      throw new Error(`画布 command 本地 checkpoint 状态损坏：${statePath}`);
    }
  };
  const copy = (value) => JSON.parse(JSON.stringify(value));
  return {
    create: (checkpoint) => enqueue(async () => {
      const state = await load();
      state.checkpoints = state.checkpoints.filter((value) => value.checkpointId !== checkpoint.checkpointId);
      state.checkpoints.push(copy(checkpoint));
      await atomicWriteJSON(statePath, state);
    }),
    get: (checkpointId) => enqueue(async () => {
      const checkpoint = (await load()).checkpoints.find((value) => value.checkpointId === checkpointId);
      return checkpoint ? copy(checkpoint) : undefined;
    }),
    list: (targetCanvasId) => enqueue(async () => copy(
      (await load()).checkpoints.filter((value) => value.canvasId === targetCanvasId)
    )),
  };
}

function createMemoryPersistence() {
  let state = emptyPersistenceState();
  const copy = (value) => JSON.parse(JSON.stringify(value));
  return {
    async appendOutbound(envelope) { state.outbound.push(copy(envelope)); },
    async clear() { state = emptyPersistenceState(); },
    async loadClientId() { return state.clientId; },
    async loadOutbound() { return copy(state.outbound); },
    async loadSnapshot() { return copy(state.snapshot); },
    async removeOutbound(txId) { state.outbound = state.outbound.filter((value) => value.txId !== txId); },
    async replaceOutbound(envelopes) { state.outbound = copy(envelopes); },
    async replaceOutboundPartition(active, capturedTxIds) {
      const replaced = new Set([...capturedTxIds, ...active.map((value) => value.txId)]);
      state.outbound = [...copy(active), ...state.outbound.filter((value) => !replaced.has(value.txId))];
    },
    async saveClientId(clientId) { state.clientId = clientId; },
    async saveSnapshot(snapshot) { state.snapshot = copy(snapshot); },
    async quarantine() { return ""; },
  };
}

function requestFetch(url, init = {}, redirects = 0) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      reject(new Error(`不支持的文本资产下载协议：${parsed.protocol}`));
      return;
    }
    const request = (parsed.protocol === "https:" ? https : http).get(parsed, (response) => {
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        response.resume();
        if (redirects >= 3) return reject(new Error("文本资产下载重定向次数过多"));
        return resolve(requestFetch(new URL(response.headers.location, parsed).toString(), init, redirects + 1));
      }
      const chunks = [];
      let bytes = 0;
      response.on("data", (chunk) => {
        bytes += chunk.length;
        if (bytes > MAX_INPUT_BYTES) {
          request.destroy(new Error("文本资产超过 64 MiB 限制"));
          return;
        }
        chunks.push(chunk);
      });
      response.on("end", () => {
        const body = Buffer.concat(chunks).toString("utf8");
        resolve({
          ok: response.statusCode >= 200 && response.statusCode < 300,
          status: response.statusCode,
          statusText: response.statusMessage || "",
          text: async () => body,
        });
      });
    });
    if (init.signal) {
      if (init.signal.aborted) request.destroy(new Error("文本资产下载已取消"));
      else init.signal.addEventListener("abort", () => request.destroy(new Error("文本资产下载已取消")), { once: true });
    }
    request.setTimeout(30_000, () => request.destroy(new Error("文本资产下载超时")));
    request.once("error", reject);
  });
}

function createCanvasAssetRuntime({ nativeClient, sdk }) {
  const serviceTransport = sdk.createPippitAssetServiceTransport({
    assetQuery: async (request) => ({
      data: { Assets: await nativeClient.getAssetsAllowMissing(request.pippit_asset_ids) },
      ret: 0,
    }),
    mutation: {
      batchGeneratePippitAssetIds: async ({ count = 0 }) => ({
        data: { ids: await nativeClient.allocateAssetIds(count) },
        ret: 0,
      }),
      batchPatchAsset: (request) => nativeClient.apply(request),
    },
  });
  const assetRuntime = sdk.createPippitAssetRuntime({
    storage: false,
    sync: { batchPatchAsset: (request) => nativeClient.apply(request) },
    transport: serviceTransport,
  });
  const loader = sdk.createPippitAssetSdkCanvasLoader({ client: assetRuntime.client });
  return { assetRuntime, loader };
}

function createCanvasTransportFactory({ assetRuntime, loader }) {
  return ({ canvasId, clientId }) => {
    const transport = assetRuntime.createSyncTransport({
      clientId,
      loader,
      scopeAssetId: canvasId,
    });
    return {
      assetVersions: transport.assetVersions,
      clientId: transport.clientId,
      close: transport.close.bind(transport),
      commit: transport.commit.bind(transport),
      fetchAssets: transport.fetchAssets.bind(transport),
      fetchSnapshot: async (options) => {
        const snapshot = await transport.fetchSnapshot(options);
        return { document: snapshot.state };
      },
      getConnectionState: transport.getConnectionState.bind(transport),
      onConnectionChange: transport.onConnectionChange.bind(transport),
      onInvalidate: transport.onInvalidate.bind(transport),
      syncAssetSubscriptions: transport.syncAssetSubscriptions.bind(transport),
    };
  };
}

function createSchemaValue(json, optional = false) {
  const schema = { ...json };
  Object.defineProperty(schema, OPTIONAL_SCHEMA, { value: optional });
  Object.defineProperties(schema, {
    describe: {
      value(description) {
        return createSchemaValue({ ...json, description }, optional);
      },
    },
    optional: {
      value() {
        return createSchemaValue(json, true);
      },
    },
  });
  return schema;
}

function createSchemaFactory() {
  return {
    array: (item) => createSchemaValue({ items: schemaToJSON(item), type: "array" }),
    boolean: () => createSchemaValue({ type: "boolean" }),
    number: () => createSchemaValue({ type: "number" }),
    string: () => createSchemaValue({ type: "string" }),
    unknown: () => createSchemaValue({}),
  };
}

function schemaToJSON(schema) {
  return Object.fromEntries(Object.entries(schema || {}).filter(([, value]) => typeof value !== "function"));
}

function definitionToJSON(name, definition) {
  const properties = {};
  const required = [];
  for (const [argument, schema] of Object.entries(definition.args || {})) {
    properties[argument] = schemaToJSON(schema);
    if (!schema[OPTIONAL_SCHEMA]) required.push(argument);
  }
  return {
    description: definition.description,
    input_schema: {
      properties,
      ...(required.length ? { required } : {}),
      type: "object",
    },
    name,
  };
}

function createDefinitions(sdk, runtime, allocateNodeId) {
  const definitions = sdk.createXyqCanvasOpencodeToolDefinitions({
    allocateNodeId,
    runtime,
    schema: createSchemaFactory(),
  });
  for (const [name, definition] of Object.entries(definitions || {})) {
    if (!definition || typeof definition.description !== "string" || typeof definition.execute !== "function") {
      throw new Error(`画布 SDK 返回了无效的 command 定义：${name}`);
    }
  }
  return definitions;
}

function ensureNodeRuntimeGlobals() {
  if (typeof globalThis.structuredClone !== "function") {
    const { deserialize, serialize } = require("v8");
    Object.defineProperty(globalThis, "structuredClone", {
      configurable: true,
      value: (value) => deserialize(serialize(value)),
      writable: true,
    });
  }
  if (typeof globalThis.fetch !== "function") {
    Object.defineProperty(globalThis, "fetch", {
      configurable: true,
      value: requestFetch,
      writable: true,
    });
  }
}

async function loadSdk(options) {
  ensureNodeRuntimeGlobals();
  if (options.sdk) return options.sdk;
  if (!fs.existsSync(DEFAULT_SDK_MODULE)) {
    throw new Error("当前 CLI 安装包缺少画布 command 运行时，请更新到包含该能力的正式版本");
  }
  const imported = await import(pathToFileURL(DEFAULT_SDK_MODULE).href);
  return imported.default && typeof imported.default === "object"
    ? { ...imported.default, ...imported }
    : imported;
}

function assertSdkFunctions(sdk, names) {
  for (const name of names) {
    if (typeof sdk[name] !== "function") throw new Error(`画布 SDK 产物缺少 ${name}`);
  }
}

function parseInputJSON(parsed, cwd) {
  let payload = parsed.input;
  if (parsed.filePath) {
    if (parsed.filePath === "-") {
      payload = fs.readFileSync(0, { encoding: "utf8" });
    } else {
      const inputPath = path.resolve(cwd, parsed.filePath);
      const stat = fs.statSync(inputPath);
      if (!stat.isFile()) throw new Error(`command 输入不是普通文件：${inputPath}`);
      if (stat.size > MAX_INPUT_BYTES) throw new Error("command 输入超过 64 MiB 限制");
      payload = fs.readFileSync(inputPath, "utf8");
    }
  }
  if (!payload) return {};
  if (Buffer.byteLength(payload) > MAX_INPUT_BYTES) throw new Error("command 输入超过 64 MiB 限制");
  let value;
  try {
    value = JSON.parse(payload);
  } catch (error) {
    throw new Error(`command 输入不是有效 JSON：${error.message}`);
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("command 输入必须是 JSON object");
  }
  return value;
}

function writeJSON(stream, value) {
  stream.write(`${JSON.stringify(value, null, 2)}\n`);
}

function decorateCatalogEntry(sdk, entry) {
  const output = { ...entry };
  if (entry.name === "apply_mutations") {
    output.mutation_definitions = sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS;
  }
  if (entry.name === "apply_mutations" || entry.name === "invoke_command") {
    output.registered_commands = sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS;
  }
  return output;
}

function createPublicCatalog(sdk, definitions) {
  const entries = [];
  const routes = new Map();
  const append = (entry, route) => {
    if (routes.has(entry.name)) return;
    entries.push(decorateCatalogEntry(sdk, entry));
    routes.set(entry.name, route);
  };
  for (const [name, definition] of Object.entries(definitions)) {
    append(definitionToJSON(name, definition), { kind: "tool", name });
  }
  for (const definition of sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS) {
    append({
      description: definition.description,
      input_schema: { description: definition.input, type: "object" },
      name: definition.kind,
    }, { kind: "mutation", name: definition.kind });
  }
  for (const definition of sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS) {
    append({
      description: definition.description,
      input_schema: { description: definition.input, type: "object" },
      name: definition.name,
    }, { kind: "registered", name: definition.name });
  }
  return { entries, routes };
}

function validatePublicCommandInput(route, input) {
  if (
    route.kind === "tool" &&
    route.name === "apply_mutations" &&
    Array.isArray(input.mutations) &&
    input.mutations.length > 1 &&
    input.mutations.some((mutation) => mutation?.kind === "invoke_command")
  ) {
    throw new Error("已注册业务 command 不能与其他 mutation 放在同一原子批次中，请单独执行");
  }
}

function executePublicCommand(route, definitions, input) {
  if (route.kind === "tool") {
    return definitions[route.name].execute(
      route.name === "apply_mutations" ? { ...input, atomic: true } : input
    );
  }
  const mutation = route.kind === "mutation"
    ? { ...input, kind: route.name }
    : { args: [input], kind: "invoke_command", name: route.name };
  return definitions.apply_mutations.execute({
    atomic: true,
    intent: `canvas command ${route.name}`,
    mutations: [mutation],
  });
}

async function runCanvasCommand(args, options = {}) {
  const parsed = parseCanvasCommandArgs(args);
  const stdout = options.stdout || process.stdout;
  const stderr = options.stderr || process.stderr;
  if (parsed.action === "help") {
    stdout.write(COMMAND_HELP);
    return 0;
  }

  const sdk = await loadSdk(options);
  assertSdkFunctions(sdk, ["createXyqCanvasOpencodeToolDefinitions"]);
  if (!Array.isArray(sdk.XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS)) {
    throw new Error("画布 SDK 产物缺少 mutation command 目录");
  }
  if (!Array.isArray(sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS)) {
    throw new Error("画布 SDK 产物缺少已注册业务 command 目录");
  }
  const catalogDefinitions = createDefinitions(
    sdk,
    { permissions: [], store: {} },
    async () => ""
  );
  const { entries: catalog, routes } = createPublicCatalog(sdk, catalogDefinitions);
  if (parsed.action === "list") {
    writeJSON(stdout, { commands: catalog });
    return 0;
  }
  const catalogEntry = catalog.find((entry) => entry.name === parsed.commandName);
  if (!catalogEntry) throw new Error(`未知的画布 command：${parsed.commandName}`);
  if (parsed.action === "describe") {
    writeJSON(stdout, catalogEntry);
    return 0;
  }

  assertSdkFunctions(sdk, [
    "createPippitAssetRuntime",
    "createPippitAssetSdkCanvasLoader",
    "createPippitAssetServiceTransport",
    "createXyqCanvasCommandRuntime",
  ]);
  const cwd = options.cwd || process.cwd();
  const input = parseInputJSON(parsed, cwd);
  const route = routes.get(parsed.commandName);
  validatePublicCommandInput(route, input);
  const nativeClient = new NativeCanvasClient(options.nativeInvocation, { cwd, stderr });
  const authStatus = await nativeClient.ensureCanvasAccess(parsed.canvasId);
  const requiresDurableCheckpoint = CHECKPOINT_COMMANDS.has(parsed.commandName) ||
    (parsed.commandName === "apply_mutations" && input.checkpointBefore === true);
  if (requiresDurableCheckpoint && !authStatus.credential_scope) {
    throw new Error("checkpoint command 需要通过网页登录，以便安全隔离跨进程状态");
  }
  const { assetRuntime, loader } = createCanvasAssetRuntime({ nativeClient, sdk });
  const checkpointStore = authStatus.credential_scope
    ? createFileCheckpointStore({
        canvasId: parsed.canvasId,
        credentialScope: authStatus.credential_scope,
        stateDirectory: options.stateDirectory,
      })
    : undefined;

  let executionError;
  let saveError;
  let serializedResult;
  const allocatedAssetIds = [];
  let standalone;
  const persistence = authStatus.credential_scope
    ? createFilePersistence({
        canvasId: parsed.canvasId,
        credentialScope: authStatus.credential_scope,
        stateDirectory: options.stateDirectory,
      })
    : createMemoryPersistence();
  try {
    standalone = sdk.createXyqCanvasCommandRuntime({
      canvasId: parsed.canvasId,
      persistence,
      sync: { flush: { maxAttempts: 1, maxBatchSize: 1 } },
      transportFactory: createCanvasTransportFactory({ assetRuntime, loader }),
    });
    if (!standalone?.canvas || !standalone.store || !standalone.commands || !standalone.runtime) {
      throw new Error("画布 SDK 返回了不完整的 command 运行时");
    }
    standalone.canvas.start();
    await standalone.canvas.whenReady();
    const definitions = createDefinitions(
      sdk,
      { checkpoints: checkpointStore, permissions: CANVAS_COMMAND_PERMISSIONS, store: standalone.store },
      async () => {
        const [assetId] = await assetRuntime.client.ids.allocate(1);
        allocatedAssetIds.push(assetId);
        return assetId;
      }
    );
    try {
      serializedResult = await executePublicCommand(route, definitions, input);
    } catch (error) {
      executionError = error;
    }
    if (!executionError) {
      try {
        standalone.canvas.flush();
        await standalone.canvas.waitUntilSaved();
      } catch (error) {
        saveError = error;
      }
    }
  } finally {
    standalone?.canvas?.dispose();
    assetRuntime.dispose();
  }
  if (executionError) {
    const quarantinePath = await persistence.quarantine();
    const suffix = quarantinePath ? `；未确认事务已隔离到 ${quarantinePath}` : "";
    throw new Error(`${executionError.message}${suffix}。请先查询画布状态，不要直接重跑该 command。`);
  }
  if (saveError) {
    const quarantinePath = await persistence.quarantine();
    const suffix = quarantinePath ? `；未确认事务已隔离到 ${quarantinePath}` : "";
    throw new Error(`${saveError.message}${suffix}。请先查询画布状态，不要直接重跑该 command。`);
  }
  if (typeof serializedResult !== "string") throw new Error("画布 command 未返回 JSON 字符串");
  let result;
  try {
    result = JSON.parse(serializedResult);
  } catch (error) {
    throw new Error(`画布 command 返回了无效 JSON：${error.message}`);
  }
  if (result?.ok === true && allocatedAssetIds.length) {
    result = { ...result, allocated_asset_ids: allocatedAssetIds };
  }
  writeJSON(stdout, result);
  return result?.ok === false ? 1 : 0;
}

module.exports = {
  COMMAND_HELP,
  NativeCanvasClient,
  createCanvasAssetRuntime,
  createCanvasTransportFactory,
  createFileCheckpointStore,
  createFilePersistence,
  createMemoryPersistence,
  createSchemaFactory,
  definitionToJSON,
  ensureNodeRuntimeGlobals,
  isCanvasCommand,
  parseCanvasCommandArgs,
  runCanvasCommand,
};

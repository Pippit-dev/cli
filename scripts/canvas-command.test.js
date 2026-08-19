"use strict";

const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");

const {
  createFilePersistence,
  ensureNodeRuntimeGlobals,
  isCanvasCommand,
  parseCanvasCommandArgs,
  runCanvasCommand,
} = require("./canvas-command");

function outputBuffer() {
  return {
    value: "",
    write(chunk) {
      this.value += chunk;
    },
  };
}

function camelFormat(value) {
  if (value?.root_canvas_id) return { ...value, rootCanvasId: value.root_canvas_id };
  if (value?.basic_info) {
    return { ...value, basicInfo: value.basic_info, nodeName: value.node_name };
  }
  return value;
}

function createFakeSdk() {
  class FakeSyncTransport {
    constructor(options, batchPatchAsset) {
      this.options = options;
      this.batchPatchAsset = batchPatchAsset;
      this.assetVersions = { getVersion: () => 5, getVersions: () => ({ canvas_1: 5 }), subscribe: () => () => {} };
      this.clientId = options.clientId;
    }
    close() {}
    async fetchAssets(requests) {
      const loaded = await this.options.loader.fetchAssets({ requests, scopeAssetId: this.options.scopeAssetId });
      return { assets: loaded.assets };
    }
    async fetchSnapshot({ onProgress } = {}) {
      const loaded = await this.options.loader.fetchSnapshot({ onProgress, scopeAssetId: this.options.scopeAssetId });
      return { state: loaded.state };
    }
    getConnectionState() { return "open"; }
    onConnectionChange() { return () => {}; }
    onInvalidate() { return () => {}; }
    syncAssetSubscriptions() {}
    async commit(payload) {
      const envelopes = payload.envelopes || [payload];
      const request = {
        batch_id: `batch_${payload.txId}`,
        client_id: this.options.clientId,
        root_pippit_asset_id: this.options.scopeAssetId,
        transactions: envelopes.map((envelope) => ({
          attempt: envelope.attempt,
          enqueued_at: envelope.enqueuedAt,
          patches: envelope.patches.map((patch) => ({
            asset_id: patch.assetId,
            base_asset_version: 5,
            op: patch.op,
            path: patch.path,
            value: patch.value,
          })),
          transaction_id: envelope.txId,
        })),
      };
      try {
        await this.batchPatchAsset(request);
        return { status: "ack" };
      } catch (error) {
        return { error, status: "transport-error" };
      }
    }
  }

  return {
    XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS: [
      { description: "创建业务节点", input: "{nodeKind}", kind: "create_biz_node" },
    ],
    XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS: [
      { description: "更新角色", input: "{nodeId, value}", name: "role.update" },
    ],
    createPippitAssetServiceTransport(options) {
      assert.strictEqual(typeof options.mutation.batchPatchAsset, "function");
      return {
        allocateAssetIds: async (count) => {
          const result = await options.mutation.batchGeneratePippitAssetIds({ count });
          return result.data.ids;
        },
        dispose() {},
        async queryAssets(refs) {
          const response = await options.assetQuery({ pippit_asset_ids: refs.map((ref) => ref.pippitAssetId) });
          return Promise.all(response.data.Assets.map(async (raw) => {
            let content = raw.TextInfo.Content;
            if (!content && raw.TextInfo.download_url) {
              const downloaded = await globalThis.fetch(raw.TextInfo.download_url);
              content = await downloaded.text();
            }
            return {
              asset: {
                pippitAssetId: raw.PippitAssetID,
                value: camelFormat(JSON.parse(content)),
                version: raw.version,
              },
            };
          }));
        },
      };
    },
    createPippitAssetRuntime({ sync, transport }) {
      return {
        client: {
          assets: { queryAssets: (...args) => transport.queryAssets(...args) },
          ids: { allocate: (count) => transport.allocateAssetIds(count) },
        },
        createSyncTransport: (options) => new FakeSyncTransport(options, sync.batchPatchAsset),
        dispose: () => transport.dispose(),
      };
    },
    createPippitAssetSdkCanvasLoader({ client }) {
      return {
        async fetchAssets({ requests }) {
          const results = await client.assets.queryAssets(requests.map(({ assetId }) => ({ pippitAssetId: assetId })));
          return { assetVersions: {}, assets: results.map(({ asset }, index) => ({ asset, assetId: requests[index].assetId })) };
        },
        async fetchSnapshot({ onProgress, scopeAssetId }) {
          const [root, role] = await client.assets.queryAssets([
            { pippitAssetId: scopeAssetId }, { pippitAssetId: "role_1" },
          ]);
          assert.strictEqual(root.asset.value.rootCanvasId, scopeAssetId);
          assert.strictEqual(role.asset.value.basicInfo.name, "角色一");
          assert.strictEqual(role.asset.value.nodeName, "人物节点");
          const document = root.asset.value;
          document.assets.role_1 = { content: role.asset.value, extra: {}, pippitAssetId: "role_1", type: "role" };
          onProgress?.({ document, kind: "root-ready" });
          return { assetVersions: { [scopeAssetId]: 5, role_1: 5 }, state: document };
        },
      };
    },
    createXyqCanvasCommandRuntime(options) {
      assert.strictEqual(options.sync.flush.maxBatchSize, 1);
      assert.strictEqual(options.sync.flush.maxAttempts, 1);
      const persistence = { ...options.persistence };
      for (const method of ["loadClientId", "saveClientId", "loadSnapshot", "saveSnapshot", "loadOutbound", "appendOutbound", "removeOutbound", "replaceOutbound", "clear"]) {
        assert.strictEqual(typeof persistence[method], "function", `missing persistence method ${method}`);
      }
      const transport = { ...options.transportFactory({ canvasId: options.canvasId, clientId: "client_1" }) };
      for (const method of ["commit", "fetchSnapshot", "fetchAssets", "onInvalidate", "onConnectionChange", "getConnectionState", "close", "syncAssetSubscriptions"]) {
        assert.strictEqual(typeof transport[method], "function", `missing transport method ${method}`);
      }
      const store = { pending: false, persistence, state: null };
      let save;
      const canvas = {
        runtime: { marker: "runtime" },
        store,
        start() { return this; },
        async whenReady() { store.state = (await transport.fetchSnapshot()).document; },
        flush() {
          if (!store.pending) return;
          save = transport.commit({
            attempt: 1,
            enqueuedAt: 2,
            patches: [{ assetId: options.canvasId, op: "replace", path: "/content/title", value: "新标题" }],
            status: "in-flight",
            txId: "tx_1",
          }).then(async (result) => {
            if (result.status === "ack") await persistence.removeOutbound("tx_1");
            return result;
          });
        },
        async waitUntilSaved() {
          if (!save) return;
          const result = await save;
          if (result.status !== "ack") throw result.error;
        },
        dispose() { transport.close(); },
      };
      return { canvas, commands: { marker: "commands" }, runtime: canvas.runtime, store };
    },
    createXyqCanvasOpencodeToolDefinitions({ allocateNodeId, runtime, schema }) {
      return {
        apply_mutations: {
          args: { intent: schema.string(), mutations: schema.array(schema.unknown()) },
          description: "SDK apply mutations description",
          execute: async ({ atomic, mutations }) => {
            assert.strictEqual(atomic, true);
            const created = mutations.find((mutation) => mutation.kind === "create_biz_node");
            if (created) {
              created.id = created.id || await allocateNodeId(created);
              runtime.store.pending = true;
              await runtime.store.persistence.appendOutbound({
                attempt: 0,
                enqueuedAt: 1,
                patches: [],
                status: "pending",
                txId: "tx_1",
              });
            }
            return JSON.stringify({ data: { mutations }, ok: true, revision: 1 });
          },
        },
        invoke_command: {
          args: { name: schema.string(), args: schema.array(schema.unknown()).optional() },
          description: "SDK invoke command description",
          execute: async () => JSON.stringify({ data: {}, ok: true, revision: 1 }),
        },
        create_checkpoint: {
          args: {},
          description: "SDK create checkpoint description",
          execute: async () => {
            const checkpoint = {
              canvasId: runtime.store.state.rootCanvasId,
              checkpointId: "checkpoint_1",
              createdAt: 1,
              document: runtime.store.state,
              documentHash: "hash_1",
              revision: 1,
            };
            await runtime.checkpoints.create(checkpoint);
            return JSON.stringify({ data: { checkpointId: checkpoint.checkpointId }, ok: true, revision: 1 });
          },
        },
        list_checkpoints: {
          args: {},
          description: "SDK list checkpoints description",
          execute: async () => JSON.stringify({
            data: { checkpoints: await runtime.checkpoints.list(runtime.store.state.rootCanvasId) },
            ok: true,
            revision: 1,
          }),
        },
        read_subject: {
          args: { assetId: schema.string().describe("SDK asset id") },
          description: "SDK read description",
          execute: async () => JSON.stringify({ data: { found: true }, ok: true, revision: 1 }),
        },
        create_subject: {
          args: { name: schema.string().describe("SDK subject name"), note: schema.string().optional() },
          description: "SDK create description",
          execute: async () => {
            runtime.store.pending = true;
            return JSON.stringify({ data: { id: await allocateNodeId() }, ok: true, revision: 2 });
          },
        },
        rejected: {
          args: {},
          description: "SDK rejected description",
          execute: async () => JSON.stringify({ code: "INVALID_ARGUMENT", message: "bad", ok: false, revision: 1 }),
        },
        throws_after_mutation: {
          args: {},
          description: "SDK unexpected failure fixture",
          execute: async () => {
            runtime.store.pending = true;
            await runtime.store.persistence.appendOutbound({
              attempt: 0,
              enqueuedAt: 1,
              patches: [],
              status: "pending",
              txId: "tx_throw",
            });
            throw new Error("tool exploded");
          },
        },
      };
    },
  };
}

async function main() {
  const structuredCloneDescriptor = Object.getOwnPropertyDescriptor(globalThis, "structuredClone");
  Object.defineProperty(globalThis, "structuredClone", { configurable: true, value: undefined, writable: true });
  ensureNodeRuntimeGlobals();
  assert.strictEqual(globalThis.structuredClone({ value: 1 }).value, 1);
  if (structuredCloneDescriptor) Object.defineProperty(globalThis, "structuredClone", structuredCloneDescriptor);

  assert.strictEqual(isCanvasCommand(["canvas", "command", "list"]), true);
  assert.strictEqual(isCanvasCommand(["canvas", "get"]), false);
  assert.deepStrictEqual(
    parseCanvasCommandArgs(["canvas", "command", "run", "create_biz_node", "--canvas-id=canvas_1", "--input", "{}"]),
    { action: "run", canvasId: "canvas_1", commandName: "create_biz_node", filePath: "", input: "{}" }
  );
  assert.throws(
    () => parseCanvasCommandArgs(["canvas", "command", "run", "read_subject", "--canvas-id", "1", "--input", "{}", "--file", "x"]),
    /不能同时使用/
  );

  const sdk = createFakeSdk();
  const listOut = outputBuffer();
  assert.strictEqual(await runCanvasCommand(["canvas", "command", "list"], { sdk, stdout: listOut }), 0);
  const listed = JSON.parse(listOut.value);
  assert.deepStrictEqual(listed.commands.slice(0, 2).map((command) => command.name), ["apply_mutations", "invoke_command"]);
  assert.strictEqual(listed.commands[0].description, "SDK apply mutations description");
  assert.strictEqual(listed.commands[0].mutation_definitions[0].kind, "create_biz_node");
  assert.strictEqual(listed.commands[0].registered_commands[0].name, "role.update");
  assert.ok(listed.commands.some((command) => command.name === "create_biz_node"));
  assert.ok(listed.commands.some((command) => command.name === "role.update"));

  const describeOut = outputBuffer();
  await runCanvasCommand(["canvas", "command", "describe", "apply_mutations"], { sdk, stdout: describeOut });
  const described = JSON.parse(describeOut.value);
  assert.deepStrictEqual(described.input_schema.required, ["intent", "mutations"]);
  assert.strictEqual(described.mutation_definitions[0].input, "{nodeKind}");
  const invokeOut = outputBuffer();
  await runCanvasCommand(["canvas", "command", "describe", "invoke_command"], { sdk, stdout: invokeOut });
  assert.strictEqual(JSON.parse(invokeOut.value).registered_commands[0].description, "更新角色");

  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "pippit command test "));
  assert.throws(() => createFilePersistence({ canvasId: "canvas_1", credentialScope: "" }), /credential_scope/);
  const durable = createFilePersistence({
    canvasId: "canvas_recovery",
    credentialScope: "credential_scope_secret",
    stateDirectory: path.join(tempRoot, "state"),
  });
  await durable.saveClientId("client_recovery");
  await durable.saveSnapshot({ document: { rootCanvasId: "canvas_recovery" } });
  await durable.appendOutbound({ patches: [], status: "pending", txId: "tx_recovery" });
  const recovered = createFilePersistence({
    canvasId: "canvas_recovery",
    credentialScope: "credential_scope_secret",
    stateDirectory: path.join(tempRoot, "state"),
  });
  assert.strictEqual(await recovered.loadClientId(), "client_recovery");
  assert.strictEqual((await recovered.loadOutbound())[0].txId, "tx_recovery");
  const persisted = fs.readFileSync(durable.statePath, "utf8");
  assert.strictEqual(persisted.includes("credential_scope_secret"), false);
  assert.strictEqual(path.basename(durable.statePath).includes("canvas_recovery"), false);
  assert.deepStrictEqual(fs.readdirSync(path.dirname(durable.statePath)).filter((name) => name.endsWith(".tmp")), []);
  const nativePath = path.join(tempRoot, "fake native.js");
  const logPath = path.join(tempRoot, "native.jsonl");
  const loginPath = path.join(tempRoot, "login-state");
  const authRejectPath = path.join(tempRoot, "auth-rejected");
  const inputPath = path.join(tempRoot, "input file.json");
  fs.writeFileSync(inputPath, '{"nodeKind":"role"}');
  fs.writeFileSync(nativePath, String.raw`
const fs = require("fs");
const args = process.argv.slice(2);
const input = args[1] === "apply" ? fs.readFileSync(0, "utf8") : "";
fs.appendFileSync(process.env.FAKE_COMMAND_LOG, JSON.stringify({ args, input }) + "\n");
const out = (value) => process.stdout.write(JSON.stringify(value) + "\n");
if (args[0] === "status") out({
  ...(process.env.FAKE_NO_SCOPE === "1" ? {} : { credential_scope: "scope_1" }),
  logged_in: fs.existsSync(process.env.FAKE_LOGIN_FILE),
  source: process.env.FAKE_NO_SCOPE === "1" ? "environment" : "browser",
});
else if (args[0] === "login") fs.writeFileSync(process.env.FAKE_LOGIN_FILE, "ok");
else if (args[1] === "get" && process.env.FAKE_AUTH_REJECT_FILE && !fs.existsSync(process.env.FAKE_AUTH_REJECT_FILE)) {
  fs.writeFileSync(process.env.FAKE_AUTH_REJECT_FILE, "rejected");
  process.stderr.write("HTTP 401\n");
  process.exit(7);
}
else if (args[1] === "get") {
  const ids = args.flatMap((arg, index) => arg === "--asset-id" ? [args[index + 1]] : []);
  out({ assets: ids.map((id) => id === "role_1"
    ? { PippitAssetID: id, TextInfo: { Content: "", download_url: "https://example.com/role" }, version: 5 }
    : { PippitAssetID: id, TextInfo: { Content: JSON.stringify({ root_canvas_id: id, assets: { [id]: { content: { edges: {}, nodes: {} }, extra: {}, pippitAssetId: id, type: "canvas" } } }) }, version: 5 }) });
} else if (args[1] === "allocate") out({ asset_ids: ["allocated_role"] });
else if (args[1] === "apply") {
  if (process.env.FAKE_APPLY_FAILURE === "1") { process.stderr.write("apply failed safely\n"); process.exit(7); }
  const request = JSON.parse(input);
  out({ results: [{ asset_versions: { canvas_1: 6 }, status: "ack", transaction_id: request.transactions[0].transaction_id }] });
} else { process.stderr.write("unexpected native command\n"); process.exit(9); }
`);
  const previousLog = process.env.FAKE_COMMAND_LOG;
  const previousLogin = process.env.FAKE_LOGIN_FILE;
  process.env.FAKE_COMMAND_LOG = logPath;
  process.env.FAKE_LOGIN_FILE = loginPath;
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async (url) => {
    assert.strictEqual(url, "https://example.com/role");
    return { ok: true, text: async () => '{"basic_info":{"name":"角色一"},"node_name":"人物节点"}' };
  };
  const nativeInvocation = { command: process.execPath, prefixArgs: [nativePath] };
  const runOut = outputBuffer();
  try {
    const exitCode = await runCanvasCommand(
      ["canvas", "command", "run", "create_biz_node", "--canvas-id", "canvas_1", "--file", inputPath],
      { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: runOut }
    );
    assert.strictEqual(exitCode, 0);
    assert.deepStrictEqual(JSON.parse(runOut.value).allocated_asset_ids, ["allocated_role"]);
    const calls = fs.readFileSync(logPath, "utf8").trim().split("\n").map(JSON.parse);
    assert.deepStrictEqual(calls.slice(0, 2).map((call) => call.args), [["status"], ["login"]]);
    const apply = calls.find((call) => call.args[1] === "apply");
    assert.deepStrictEqual(apply.args, ["canvas", "apply", "--transport-result", "--file", "-"]);
    assert.strictEqual(JSON.parse(apply.input).transactions.length, 1);

    const checkpointOut = outputBuffer();
    await runCanvasCommand(
      ["canvas", "command", "run", "create_checkpoint", "--canvas-id", "canvas_1", "--input", "{}"],
      { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: checkpointOut }
    );
    assert.strictEqual(JSON.parse(checkpointOut.value).data.checkpointId, "checkpoint_1");
    const checkpointsOut = outputBuffer();
    await runCanvasCommand(
      ["canvas", "command", "run", "list_checkpoints", "--canvas-id", "canvas_1", "--input", "{}"],
      { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: checkpointsOut }
    );
    assert.strictEqual(JSON.parse(checkpointsOut.value).data.checkpoints[0].checkpointId, "checkpoint_1");

    const registeredOut = outputBuffer();
    await runCanvasCommand(
      ["canvas", "command", "run", "role.update", "--canvas-id", "canvas_1", "--input", '{"value":"新名称"}'],
      { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: registeredOut }
    );
    const invoked = JSON.parse(registeredOut.value).data.mutations[0];
    assert.deepStrictEqual(invoked, { args: [{ value: "新名称" }], kind: "invoke_command", name: "role.update" });

    await assert.rejects(
      runCanvasCommand(
        [
          "canvas", "command", "run", "apply_mutations", "--canvas-id", "canvas_1", "--input",
          '{"intent":"unsupported-chain","mutations":[{"kind":"set_title","title":"新标题"},{"kind":"invoke_command","name":"role.update","args":[{}]}]}',
        ],
        { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: outputBuffer() }
      ),
      /不能与其他 mutation 放在同一原子批次/
    );

    process.env.FAKE_NO_SCOPE = "1";
    await assert.rejects(
      runCanvasCommand(
        ["canvas", "command", "run", "apply_mutations", "--canvas-id", "canvas_1", "--input", '{"checkpointBefore":true,"intent":"test","mutations":[]}'],
        { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: outputBuffer() }
      ),
      /需要通过网页登录/
    );
    delete process.env.FAKE_NO_SCOPE;

    process.env.FAKE_AUTH_REJECT_FILE = authRejectPath;
    const rejectOut = outputBuffer();
    const rejected = await runCanvasCommand(
      ["canvas", "command", "run", "rejected", "--canvas-id", "canvas_1", "--input", "{}"],
      { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: rejectOut }
    );
    assert.strictEqual(rejected, 1);
    assert.strictEqual(JSON.parse(rejectOut.value).ok, false);
    const refreshedCalls = fs.readFileSync(logPath, "utf8").trim().split("\n").map(JSON.parse);
    assert.ok(refreshedCalls.some((call) => call.args.join(" ") === "login --force"));
    delete process.env.FAKE_AUTH_REJECT_FILE;
    delete process.env.FAKE_NO_SCOPE;

    const applyCountBeforeThrow = fs.readFileSync(logPath, "utf8").trim().split("\n")
      .map(JSON.parse)
      .filter((call) => call.args[1] === "apply").length;
    await assert.rejects(
      runCanvasCommand(
        ["canvas", "command", "run", "throws_after_mutation", "--canvas-id", "canvas_1", "--input", "{}"],
        { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: outputBuffer() }
      ),
      /tool exploded.*不要直接重跑/
    );
    const applyCountAfterThrow = fs.readFileSync(logPath, "utf8").trim().split("\n")
      .map(JSON.parse)
      .filter((call) => call.args[1] === "apply").length;
    assert.strictEqual(applyCountAfterThrow, applyCountBeforeThrow);

    process.env.FAKE_APPLY_FAILURE = "1";
    await assert.rejects(
      runCanvasCommand(
        ["canvas", "command", "run", "create_biz_node", "--canvas-id", "canvas_1", "--input", '{"nodeKind":"role"}'],
        { cwd: tempRoot, nativeInvocation, sdk, stateDirectory: path.join(tempRoot, "state"), stdout: outputBuffer() }
      ),
      /apply failed safely.*不要直接重跑/
    );
    const quarantined = fs.readdirSync(path.join(tempRoot, "state"))
      .filter((name) => name.includes(".ambiguous.") && name.endsWith(".json"));
    assert.strictEqual(quarantined.length, 2);
    const quarantinedTransactionIds = quarantined.flatMap((name) => JSON.parse(
      fs.readFileSync(path.join(tempRoot, "state", name), "utf8")
    ).outbound.map((envelope) => envelope.txId));
    assert.deepStrictEqual(quarantinedTransactionIds.sort(), ["tx_1", "tx_throw"]);
    delete process.env.FAKE_APPLY_FAILURE;
    delete process.env.FAKE_AUTH_REJECT_FILE;
  } finally {
    if (previousLog === undefined) delete process.env.FAKE_COMMAND_LOG;
    else process.env.FAKE_COMMAND_LOG = previousLog;
    if (previousLogin === undefined) delete process.env.FAKE_LOGIN_FILE;
    else process.env.FAKE_LOGIN_FILE = previousLogin;
    globalThis.fetch = previousFetch;
    delete process.env.FAKE_APPLY_FAILURE;
    fs.rmSync(tempRoot, { force: true, recursive: true });
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

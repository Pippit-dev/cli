"use strict";

const assert = require("assert");
const path = require("path");
const { createSchemaFactory, ensureNodeRuntimeGlobals } = require("./canvas-command");

async function main() {
  ensureNodeRuntimeGlobals();
  const sdk = require(
    path.resolve(process.argv[2] || path.join(__dirname, "../dist/xyq-canvas-command-runtime.cjs")),
  );
  const names = sdk.XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS.map(({ name }) => name);
  for (const name of ["xyq.scene3d.apply", "xyq.scene3d.query"]) {
    assert(!names.includes(name), `${name} must remain disabled until a V2 adapter is registered`);
  }
  const runtime = sdk.createXyqCanvasCommandRuntime({
    canvasId: "scene3d-v2-boundary",
    transportFactory() {
      throw new Error("V2 boundary checks must remain offline");
    },
  });
  const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
    schema: createSchemaFactory(),
    runtime: {
      store: runtime.store,
      permissions: ["canvas.read", "canvas.write", "canvas.asset.read", "canvas.asset.write"],
    },
  });
  try {
    const created = JSON.parse(
      await tools.apply_mutations.execute({
        atomic: true,
        mutations: [{ kind: "create_biz_node", nodeKind: "scene3d", id: "director" }],
      }),
    );
    assert.strictEqual(created.ok, true, JSON.stringify(created));
    const assets = runtime.store.getState().assets;
    const reference = assets.director.content.documentRef;
    assert.strictEqual(reference.schemaVersion, 2);
    const document = assets[reference.pippitAssetId].content;
    assert.strictEqual(document.version, 2);
    assert(Object.keys(document.nodesById).length > 0);
    assert.strictEqual(document.nodes, undefined, "factory must not produce a legacy V1 document");
    const before = JSON.stringify(runtime.store.getState());
    const rejected = JSON.parse(
      await tools.apply_mutations.execute({
        atomic: true,
        mutations: [
          {
            kind: "invoke_command",
            name: "xyq.scene3d.apply",
            args: [
              {
                nodeId: "director",
                operations: [{ command: "remove_node", args: { id: "camera_1" } }],
              },
            ],
          },
        ],
      }),
    );
    assert.strictEqual(rejected.ok, false, "retired V1 command must not edit a V2 document");
    assert.strictEqual(JSON.stringify(runtime.store.getState()), before);
    assert.strictEqual(tools["xyq.scene3d.query"], undefined);
  } finally {
    runtime.canvas.dispose();
  }
  console.log(
    "Compiled Scene3D V2 boundary passed: V2 creation, retired commands absent and rejected without writes",
  );
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

"use strict";

const assert = require("assert");
const { definitionToJSON, runCanvasCommand } = require("./canvas-command");

const inputSchema = {
  type: "object",
  required: ["nodeKind"],
  properties: {
    nodeKind: { type: "string", enum: ["scene3d", "timeline-composition"] },
    initialData: {
      type: "object",
      additionalProperties: true,
    },
  },
  allOf: [
    {
      if: { properties: { nodeKind: { const: "scene3d" } }, required: ["nodeKind"] },
      then: {
        properties: {
          initialData: {
            type: "object",
            properties: { sceneTitle: { type: "string", default: "" } },
          },
        },
      },
    },
    {
      if: { properties: { nodeKind: { const: "timeline-composition" } }, required: ["nodeKind"] },
      then: {
        properties: {
          initialData: {
            type: "object",
            properties: { draftMetadata: { type: "string" } },
          },
        },
      },
    },
  ],
};
const vectorSchema = {
  type: "object",
  properties: { x: { type: "number" }, y: { type: "number" }, z: { type: "number" } },
  required: ["x", "y", "z"],
  additionalProperties: false,
};
const operationSchema = {
  type: "object",
  required: ["nodeId", "operations"],
  properties: {
    nodeId: { type: "string" },
    operations: {
      type: "array",
      items: {
        oneOf: [
          {
            type: "object",
            required: ["command", "args"],
            properties: {
              command: { const: "update_transform" },
              args: {
                type: "object",
                required: ["nodeId", "transform"],
                properties: {
                  nodeId: { type: "string" },
                  transform: { type: "object", properties: { position: vectorSchema } },
                },
              },
            },
          },
          {
            type: "object",
            required: ["command", "args"],
            properties: {
              command: { const: "rename_node" },
              args: {
                type: "object",
                required: ["nodeId", "name"],
                properties: { nodeId: { type: "string" }, name: { type: "string" } },
              },
            },
          },
        ],
      },
    },
  },
};
const timelineSchema = {
  type: "object",
  required: ["nodeId", "expectedRevision", "commands"],
  properties: {
    nodeId: { type: "string" },
    expectedRevision: { type: "integer", minimum: 0 },
    commands: {
      type: "array",
      items: {
        oneOf: [
          {
            type: "object",
            required: ["type", "payload"],
            properties: {
              type: { const: "set_output_size" },
              payload: {
                type: "object",
                required: ["width", "height"],
                properties: {
                  width: { type: "integer", minimum: 1 },
                  height: { type: "integer", minimum: 1 },
                },
              },
            },
          },
        ],
      },
    },
  },
};

function assertNoRepeatedCatalog(entry) {
  assert.strictEqual(entry.mutation_definitions, undefined);
  assert.strictEqual(entry.registered_commands, undefined);
}

async function main() {
  let executions = 0;
  let runtimeCreations = 0;
  let nativeLookups = 0;
  const originalSchemas = JSON.stringify({ inputSchema, operationSchema, timelineSchema });
  const sdk = {
    XYQ_CANVAS_OPENCODE_MUTATION_DEFINITIONS: [
      { kind: "create_biz_node", description: "Create node", input: "{nodeKind}", inputSchema },
    ],
    XYQ_CANVAS_REGISTERED_COMMAND_DEFINITIONS: [
      { name: "xyq.scene3d.apply", description: "Edit scene", input: "{operations}", inputSchema: operationSchema },
      { name: "xyq.timeline.apply", description: "Edit timeline", input: "{commands}", inputSchema: timelineSchema },
    ],
    createXyqCanvasCommandRuntime() {
      runtimeCreations += 1;
      throw new Error("Discovery must not create a document runtime");
    },
    createXyqCanvasOpencodeToolDefinitions() {
      return {
        "xyq.scene3d.query": {
          args: {},
          description: "Query scene",
          inputSchema,
          async execute() {
            executions += 1;
            throw new Error("Discovery must not execute commands");
          },
        },
      };
    },
  };
  async function discover(args) {
    let output = "";
    const options = {
      sdk,
      stdout: {
        write(value) {
          output += value;
        },
      },
    };
    Object.defineProperty(options, "nativeInvocation", {
      get() {
        nativeLookups += 1;
        throw new Error("Discovery must not request authentication or a native process");
      },
    });
    const status = await runCanvasCommand(["canvas", "command", ...args], options);
    assert.strictEqual(status, 0);
    if (args[0] !== "schema") {
      const budget = args[0] === "list" ? 16 * 1024 : 32 * 1024;
      assert(Buffer.byteLength(output) <= budget, `Discovery budget exceeded: ${args.join(" ")}`);
    }
    return JSON.parse(output);
  }

  const listed = await discover(["list"]);
  assert.strictEqual(listed.schema_version, 2);
  assert(listed.categories, "The index must expose its category choices");
  const { commands } = listed;
  assert.strictEqual(commands.length, 4);
  for (const entry of commands) {
    assert.strictEqual(typeof entry.summary, "string");
    assert(entry.summary.length > 0);
    assert.strictEqual(typeof entry.category, "string");
    assert(entry.category.length > 0);
    assert.strictEqual(entry.input_schema, undefined, "list is a lightweight index");
    assertNoRepeatedCatalog(entry);
    const described = await discover(["describe", entry.name]);
    assert.strictEqual(described.schema_version, 2);
    assert.strictEqual(described.name, entry.name);
    assert.strictEqual(described.category, entry.category);
    assert.strictEqual(described.schema_view, "summary");
    assert(described.input_schema);
    assert(described.schema_export, "describe must show how to export the complete schema");
    assertNoRepeatedCatalog(described);
  }

  for (const category of new Set(commands.map((entry) => entry.category))) {
    const filtered = await discover(["list", "--category", category]);
    assert.deepStrictEqual(
      filtered.commands,
      commands.filter((entry) => entry.category === category),
    );
  }
  const exported = await discover(["schema"]);
  assert.strictEqual(exported.schema_version, 2);
  assert.deepStrictEqual(
    exported.commands.map((entry) => entry.name),
    commands.map((entry) => entry.name),
  );
  for (const entry of exported.commands) {
    const single = await discover(["schema", entry.name]);
    assert.strictEqual(single.schema_version, 2);
    assert.strictEqual(single.name, entry.name);
    assert.deepStrictEqual(single.input_schema, entry.input_schema);
    assertNoRepeatedCatalog(single);
    assertNoRepeatedCatalog(entry);
    const expected =
      entry.name === "xyq.scene3d.apply"
        ? operationSchema
        : entry.name === "xyq.timeline.apply"
          ? timelineSchema
          : inputSchema;
    assert.deepStrictEqual(entry.input_schema, expected, "Explicit export must preserve the full SDK schema");
  }

  const scene = await discover(["describe", "xyq.scene3d.apply"]);
  assert.deepStrictEqual(
    scene.operations.map((entry) => entry.name),
    ["update_transform", "rename_node"],
  );
  assert(scene.operations.every((entry) => typeof entry.summary === "string" && entry.summary.length));
  const operationsHint = scene.input_schema.properties.operations.items;
  assert.strictEqual(operationsHint.schema_path, "properties.operations.items");
  assert.strictEqual(operationsHint.expand, true);
  const variants = await discover(["describe", "xyq.scene3d.apply", "--path", operationsHint.schema_path]);
  assert.deepStrictEqual(
    variants.input_schema.oneOf.map((entry) => entry.selectors.command),
    ["update_transform", "rename_node"],
  );
  const firstVariant = await discover([
    "describe",
    "xyq.scene3d.apply",
    "--path",
    variants.input_schema.oneOf[0].schema_path,
  ]);
  assert.strictEqual(
    firstVariant.input_schema.properties.command.const,
    "update_transform",
    "Advertised expansion paths must be directly usable, including array branches",
  );
  const filteredScene = await discover(["describe", "xyq.scene3d.apply", "--operation", "update_transform"]);
  const selectedScene = filteredScene.input_schema.properties.operations.items;
  assert.strictEqual(selectedScene.oneOf, undefined);
  assert.strictEqual(selectedScene.properties.command.const, "update_transform");
  const argsPath = "properties.operations.items.properties.args";
  const args = await discover(["describe", "xyq.scene3d.apply", "--operation", "update_transform", "--path", argsPath]);
  assert.deepStrictEqual(args.input_schema.required, ["nodeId", "transform"]);
  assert.strictEqual(args.input_schema.properties.nodeId.type, "string");
  const vector = await discover([
    "describe",
    "xyq.scene3d.apply",
    "--operation",
    "update_transform",
    "--path",
    `${argsPath}.properties.transform.properties.position`,
  ]);
  assert.deepStrictEqual(vector.input_schema.required, ["x", "y", "z"]);
  assert.strictEqual(vector.input_schema.properties.x.type, "number");

  const timeline = await discover(["describe", "xyq.timeline.apply"]);
  assert.deepStrictEqual(
    timeline.operations.map((entry) => entry.name),
    ["set_output_size"],
  );
  const filteredTimeline = await discover(["describe", "xyq.timeline.apply", "--operation", "set_output_size"]);
  assert.strictEqual(filteredTimeline.input_schema.properties.commands.items.properties.type.const, "set_output_size");
  const payload = await discover([
    "describe",
    "xyq.timeline.apply",
    "--operation",
    "set_output_size",
    "--path",
    "properties.commands.items.properties.payload",
  ]);
  assert.deepStrictEqual(payload.input_schema.required, ["width", "height"]);
  assert.strictEqual(payload.input_schema.properties.width.minimum, 1);

  const create = await discover(["describe", "create_biz_node"]);
  assert.deepStrictEqual(create.node_kinds, ["scene3d", "timeline-composition"]);
  for (const [kind, field, otherField] of [
    ["scene3d", "sceneTitle", "draftMetadata"],
    ["timeline-composition", "draftMetadata", "sceneTitle"],
  ]) {
    const data = await discover([
      "describe",
      "create_biz_node",
      "--node-kind",
      kind,
      "--path",
      "properties.initialData",
    ]);
    assert.strictEqual(data.input_schema.properties[field].type, "string");
    assert.strictEqual(
      data.input_schema.properties[otherField],
      undefined,
      "Other node kinds must not leak into the focused view",
    );
  }

  for (const args of [
    ["list", "--category", "unknown-category"],
    ["list", "--operation", "update_transform"],
    ["describe", "unknown-command"],
    ["describe", "xyq.scene3d.apply", "--operation", "unknown-operation"],
    ["describe", "xyq.scene3d.query", "--operation", "update_transform"],
    ["describe", "create_biz_node", "--node-kind", "unknown-kind"],
    ["describe", "xyq.scene3d.apply", "--node-kind", "scene3d"],
    ["describe", "xyq.scene3d.apply", "--path", "properties.missing"],
    ["describe", "xyq.scene3d.apply", "--path", "__proto__.constructor"],
    ["describe", "xyq.scene3d.apply", "--path", "properties.constructor"],
    ["describe", "xyq.scene3d.apply", "--operation"],
    ["schema", "unknown-command"],
    ["schema", "--input="],
    ["list", "--canvas-id="],
    ["describe", "create_biz_node", "--file="],
    ["guide", "--input", ""],
    ["schema", "xyq.scene3d.apply", "--operation", "update_transform"],
  ]) {
    await assert.rejects(() => discover(args), undefined, `Invalid discovery filter accepted: ${args.join(" ")}`);
  }
  assert.strictEqual(
    JSON.stringify({ inputSchema, operationSchema, timelineSchema }),
    originalSchemas,
    "Filtering and previews must never mutate shared SDK schemas",
  );
  assert.strictEqual(executions, 0);
  assert.strictEqual(runtimeCreations, 0);
  assert.strictEqual(nativeLookups, 0, "All discovery paths, including failures, need no authentication or writes");

  const legacy = definitionToJSON("legacy", {
    description: "Legacy runtime",
    args: { nodeId: { type: "string" } },
  });
  assert.deepStrictEqual(legacy.input_schema, {
    type: "object",
    properties: { nodeId: { type: "string" } },
    required: ["nodeId"],
  });
  console.log("Canvas command schema discovery tests passed");
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

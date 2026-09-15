"use strict";

const assert = require("assert");
const path = require("path");
const { createSchemaFactory, ensureNodeRuntimeGlobals, runCanvasCommand } = require("./canvas-command");

async function main() {
  ensureNodeRuntimeGlobals();
  const sdk = require(path.resolve(process.argv[2] || path.join(__dirname, "../dist/xyq-canvas-command-runtime.cjs")));
  let discoveryCount = 0;
  let largestDescribe = 0;
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
        throw new Error("Artifact discovery must not authenticate or invoke native commands");
      },
    });
    const status = await runCanvasCommand(["canvas", "command", ...args], options);
    assert.strictEqual(status, 0);
    const bytes = Buffer.byteLength(output);
    if (args[0] === "list") assert(bytes <= 16 * 1024, `list exceeded 16 KiB: ${bytes}`);
    if (args[0] === "describe") {
      assert(bytes <= 32 * 1024, `${args.join(" ")} exceeded 32 KiB: ${bytes}`);
      largestDescribe = Math.max(largestDescribe, bytes);
    }
    const result = JSON.parse(output);
    assert.strictEqual(result.schema_version, 2);
    discoveryCount += 1;
    return result;
  }
  const listed = await discover(["list"]);
  assert.strictEqual(listed.commands.length, 47);
  for (const command of listed.commands) {
    assert.strictEqual(command.input_schema, undefined, `${command.name} leaked its full schema into list`);
    assert.strictEqual(typeof command.summary, "string");
    assert(command.summary.length > 0);
    assert.strictEqual(typeof command.category, "string");
    assert(command.category.length > 0);
  }
  const { commands } = await discover(["schema"]);
  assert.strictEqual(commands.length, 47);
  assert.deepStrictEqual(
    commands.map(({ name }) => name),
    listed.commands.map(({ name }) => name),
  );
  const create = commands.find(({ name }) => name === "create_biz_node");
  for (const kind of ["scene3d", "timeline-composition"]) {
    assert(create.input_schema.properties.nodeKind.enum.includes(kind));
  }
  assert.strictEqual(
    commands.find(({ name }) => name === "apply_mutations").input_schema.properties.atomic.default,
    true,
  );
  for (const command of commands) {
    assert(command.input_schema.properties, `Missing properties on ${command.name}`);
    assert.strictEqual(command.mutation_definitions, undefined);
    assert.strictEqual(command.registered_commands, undefined);
    const described = await discover(["describe", command.name]);
    assert.strictEqual(described.name, command.name);
    assert.strictEqual(described.schema_view, "summary");
    assert.strictEqual(described.category, listed.commands.find(({ name }) => name === command.name).category);
    assert(described.schema_export, `Missing explicit export route on ${command.name}`);
    assert(described.input_schema, `Missing current-layer schema preview on ${command.name}`);
  }

  const sceneOperationNames = [
    "create_node",
    "remove_node",
    "rename_node",
    "update_transform",
    "update_camera",
    "set_active_camera",
    "toggle_visible",
    "toggle_lock",
    "group_nodes",
    "ungroup_node",
    "update_group_layout",
    "upsert_keyframe",
    "remove_keyframes",
    "move_keyframes",
    "set_keyframe_interpolation",
    "set_frame_range",
    "apply_path_motion",
    "update_motion_clip",
    "remove_motion_clip",
    "split_motion_clip",
    "reorder_motion_clip",
  ];
  const timelineOperationNames = [
    "insert_clip",
    "remove_clip",
    "split_clip",
    "replace_source",
    "move_clip",
    "trim_clip",
    "set_clip_speed",
    "reorder_video_clips",
    "set_clip_transform",
    "set_clip_fit",
    "set_clip_extensions",
    "set_draft_extensions",
    "set_clip_enabled",
    "set_original_audio",
    "move_clip_to_track",
    "set_audio_volume",
    "set_audio_mute",
    "set_audio_fade",
    "set_track_enabled",
    "set_track_locked",
    "set_output_size",
    "insert_audio_track",
    "insert_video_track",
    "remove_audio_track",
    "remove_video_track",
    "reorder_video_tracks",
    "set_primary_video_track",
    "insert_text_track",
    "remove_text_track",
    "set_text_track_default_style",
    "set_text_track_default_layout",
    "set_text_clip_text",
    "set_text_clip_enabled",
    "set_text_clip_style",
    "set_text_clip_layout",
    "apply_text_style_to_all",
    "replace_text_clips",
    "reflow_text_clips",
  ];
  const compareNames = (a, b) => a.localeCompare(b, "en");
  for (const { name, container, discriminator, argumentKey, expectedNames } of [
    {
      name: "xyq.scene3d.apply",
      container: "operations",
      discriminator: "command",
      argumentKey: "args",
      expectedNames: sceneOperationNames,
    },
    {
      name: "xyq.timeline.apply",
      container: "commands",
      discriminator: "type",
      argumentKey: "payload",
      expectedNames: timelineOperationNames,
    },
  ]) {
    const schema = commands.find((entry) => entry.name === name).input_schema;
    const variants = schema.properties[container].items.oneOf;
    const actualNames = variants.map((variant) => variant.properties[discriminator].const);
    assert.deepStrictEqual([...actualNames].sort(compareNames), [...expectedNames].sort(compareNames));
    const described = await discover(["describe", name]);
    assert.deepStrictEqual(
      described.operations.map((entry) => entry.name).sort(compareNames),
      [...expectedNames].sort(compareNames),
    );
    assert(described.operations.every((entry) => typeof entry.summary === "string" && entry.summary.length));
    for (const operation of actualNames) {
      const selected = await discover(["describe", name, "--operation", operation]);
      const item = selected.input_schema.properties[container].items;
      assert.strictEqual(item.oneOf, undefined, `${name}/${operation} retained unrelated variants`);
      assert.strictEqual(item.properties[discriminator].const, operation);
      const expanded = await discover([
        "describe",
        name,
        "--operation",
        operation,
        "--path",
        `properties.${container}.items.properties.${argumentKey}`,
      ]);
      assert(expanded.input_schema, `${name}/${operation} cannot expand ${argumentKey}`);
      const original = variants.find((variant) => variant.properties[discriminator].const === operation);
      assert.deepStrictEqual(
        expanded.input_schema.required,
        original.properties[argumentKey].required,
        `${name}/${operation} lost required fields while expanding`,
      );
    }
  }
  const describedCreate = await discover(["describe", "create_biz_node"]);
  const nodeKinds = create.input_schema.properties.nodeKind.enum;
  assert.strictEqual(nodeKinds.length, 10);
  assert.deepStrictEqual([...describedCreate.node_kinds].sort(compareNames), [...nodeKinds].sort(compareNames));
  for (const kind of nodeKinds) {
    const selected = await discover(["describe", "create_biz_node", "--node-kind", kind]);
    const narrowed = selected.input_schema.properties.nodeKind;
    if (narrowed.const !== undefined) assert.strictEqual(narrowed.const, kind);
    else assert.deepStrictEqual(narrowed.enum, [kind]);
    const expanded = await discover([
      "describe",
      "create_biz_node",
      "--node-kind",
      kind,
      "--path",
      "properties.initialData",
    ]);
    assert(expanded.input_schema, `Cannot discover ${kind}.initialData`);
  }
  const exportedDirector = await discover(["schema", "xyq.scene3d.apply"]);
  assert.deepStrictEqual(
    exportedDirector.input_schema,
    commands.find(({ name }) => name === "xyq.scene3d.apply").input_schema,
    "Focused discovery must not mutate the full exported schema",
  );
  const director = commands.find(({ name }) => name === "xyq.scene3d.apply");
  const createObject = director.input_schema.properties.operations.items.oneOf.find(
    (schema) => schema.properties.command.const === "create_node",
  ).properties.args;
  const primitive = createObject.oneOf.find((schema) => schema.properties.kind.const === "primitive");
  const sphere = primitive.allOf.find((schema) => schema.if.properties.primitiveKind.const === "SphereGeometry").then
    .properties.parameters;
  assert.strictEqual(sphere.properties.radius.default, 0.55);
  assert.match(sphere.properties.phiStart.description, /radians/);
  assert.strictEqual(sphere.additionalProperties, false);
  const updatePrompt = commands.find(({ name }) => name === "xyq.generation.update_prompt");
  assert.deepStrictEqual(updatePrompt.input_schema.required, ["nodeId", "prompt"]);
  assert.strictEqual(updatePrompt.input_schema.properties.prompt.type, "string");
  assert.strictEqual(updatePrompt.input_schema.properties.references, undefined);
  assert.strictEqual(updatePrompt.input_schema.additionalProperties, false);

  const runtime = sdk.createXyqCanvasCommandRuntime({
    canvasId: "artifact-test",
    transportFactory() {
      throw new Error("Pure compiled document operations must not use network");
    },
  });
  const tools = sdk.createXyqCanvasOpencodeToolDefinitions({
    schema: createSchemaFactory(),
    runtime: {
      store: runtime.store,
      permissions: ["canvas.read", "canvas.asset.read", "canvas.write", "canvas.asset.write"],
    },
  });
  async function apply(mutation) {
    const result = JSON.parse(
      await tools.apply_mutations.execute({ atomic: true, intent: "Artifact smoke", mutations: [mutation] }),
    );
    assert.strictEqual(result.ok, true, JSON.stringify(result));
  }
  try {
    await apply({ kind: "create_biz_node", nodeKind: "image", id: "prompt-target" });
    const originalCaption = runtime.store.getState().assets["prompt-target"].content.caption;
    await apply({
      kind: "create_biz_node",
      nodeKind: "image",
      id: "prompt-reference",
      initialData: { pippitAssetId: "reference-media", assetId: "reference-cloud" },
    });
    await apply({
      kind: "invoke_command",
      name: "xyq.generation.update_prompt",
      args: [{ nodeId: "prompt-target", prompt: "Use <node-asset>prompt-reference</node-asset>" }],
    });
    const promptContent = runtime.store.getState().assets["prompt-target"].content;
    assert.strictEqual(promptContent.generation.prompt, "Use <node-asset>prompt-reference</node-asset>");
    assert.strictEqual(promptContent.caption, originalCaption);
    assert(
      Object.values(runtime.store.getRootCanvasContent().edges).some(
        (edge) => edge.type === "reference" && edge.source === "prompt-reference" && edge.target === "prompt-target",
      ),
    );
    runtime.store.commands.updateNodeData("prompt-target", (draft) => {
      draft.generation.references = [
        {
          type: "image",
          pippitAssetId: "standalone-media",
          assetId: "standalone-cloud",
        },
      ];
    });
    const nodesBeforeAssetReference = Object.keys(runtime.store.getRootCanvasContent().nodes);
    const edgesBeforeAssetReference = Object.keys(runtime.store.getRootCanvasContent().edges);
    await apply({
      kind: "invoke_command",
      name: "xyq.generation.update_prompt",
      args: [
        {
          nodeId: "prompt-target",
          prompt: "Use uploaded <pippit-asset-id>standalone-media</pippit-asset-id>",
        },
      ],
    });
    const standaloneContent = runtime.store.getState().assets["prompt-target"].content;
    assert.strictEqual(
      standaloneContent.generation.prompt,
      "Use uploaded <pippit-asset-id>standalone-media</pippit-asset-id>",
    );
    assert(
      standaloneContent.generation.references.some(
        (reference) =>
          reference.pippitAssetId === "standalone-media" &&
          reference.assetId === "standalone-cloud" &&
          !reference.nodeAssetId,
      ),
    );
    assert.deepStrictEqual(Object.keys(runtime.store.getRootCanvasContent().nodes), nodesBeforeAssetReference);
    assert.deepStrictEqual(Object.keys(runtime.store.getRootCanvasContent().edges), edgesBeforeAssetReference);
    assert.strictEqual(runtime.store.getState().assets["standalone-media"], undefined);
    await apply({ kind: "create_biz_node", nodeKind: "timeline-composition", id: "timeline" });
    const timeline = JSON.parse(await tools["xyq.timeline.query"].execute({ nodeId: "timeline" }));
    assert.strictEqual(timeline.ok, true);
    await apply({
      kind: "invoke_command",
      name: "xyq.timeline.apply",
      args: [
        {
          nodeId: "timeline",
          expectedRevision: timeline.data.draft.revision,
          commands: [{ type: "set_output_size", payload: { width: 1280, height: 720 } }],
        },
      ],
    });
    const updated = JSON.parse(await tools["xyq.timeline.query"].execute({ nodeId: "timeline" }));
    assert.strictEqual(updated.data.draft.output.width, 1280);

    await apply({ kind: "create_biz_node", nodeKind: "scene3d", id: "director" });
    const input = {
      nodeId: "director",
      operations: [
        { command: "create_node", args: { kind: "camera", id: "artifact-camera", name: "Artifact camera" } },
      ],
    };
    await runtime.prepareCommand("xyq.scene3d.apply", input);
    await apply({ kind: "invoke_command", name: "xyq.scene3d.apply", args: [input] });
    const camera = JSON.parse(
      await tools["xyq.scene3d.query"].execute({ nodeId: "director", objectId: "artifact-camera" }),
    );
    assert.strictEqual(camera.ok, true);
    assert.strictEqual(camera.data.node.name, "Artifact camera");
  } finally {
    runtime.canvas.dispose();
  }
  console.log(
    `Compiled Canvas runtime smoke passed: ${commands.length} commands, 21 director/38 timeline operations; ` +
      `${discoveryCount} discovery checks, largest describe ${largestDescribe} bytes; ` +
      "prompt/reference, director and timeline edits succeeded",
  );
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

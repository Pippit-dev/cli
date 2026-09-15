"use strict";

const assert = require("assert");
const { listGuides, getGuide } = require("./canvas-command-guides");

const index = listGuides();
const expectedNames = ["storyboard", "prompt-references", "time", "timeline", "scene3d"];
assert.deepStrictEqual(
  index.map(({ name }) => name),
  expectedNames,
);
assert(Buffer.byteLength(JSON.stringify(index, null, 2), "utf8") <= 2 * 1024, "guide index must fit in 2 KiB");

for (const entry of index) {
  assert.deepStrictEqual(Object.keys(entry).sort(), ["name", "summary"], "index must not embed guide bodies");
  assert(entry.summary.length > 0);
  const guide = getGuide(entry.name);
  assert.strictEqual(guide.name, entry.name);
  assert.strictEqual(guide.summary, entry.summary);
  assert(guide.content.length > 0);
  assert(Buffer.byteLength(JSON.stringify(guide, null, 2), "utf8") <= 12 * 1024, `${entry.name} must fit in 12 KiB`);
  assert(guide.next_commands.length > 0);
  assert(guide.next_commands.every((command) => command.startsWith("pippit-tool-cli canvas ")));
  assert(guide.examples.length > 0);
  assert(guide.related.every((name) => expectedNames.includes(name)));
  assert.deepStrictEqual(JSON.parse(JSON.stringify(guide)), guide, "guide must be plain JSON");
}

const storyboard = getGuide("storyboard");
assert.match(storyboard.content, /当前没有公开的故事板脚本编辑/);
assert.match(storyboard.content, /优先采用该总和.*回退到正数 generation\.durationMs/);
assert.match(storyboard.content, /不是可执行的台词调度/);
assert.match(storyboard.content, /不能从通用 generate-video 的帮助推断/);
assert(
  !storyboard.next_commands.some((command) => /canvas apply|command run/.test(command)),
  "storyboard guide must not route users around missing domain operations",
);
const durationTags = [...storyboard.examples[0].prompt.matchAll(/<duration-ms>(\d+)<\/duration-ms>/g)];
assert.strictEqual(durationTags.length, 2);
assert(durationTags.every((match) => Number(match[1]) > 0));
assert.strictEqual(
  durationTags.reduce((total, match) => total + Number(match[1]), 0),
  5000,
);
assert.match(storyboard.examples[0].prompt, /<node-asset>ROLE_NODE_ASSET_ID<\/node-asset>/);

const references = getGuide("prompt-references");
assert.match(references.content, /可以没有节点/);
assert.match(references.content, /不会查询未知媒体并加入草稿/);
assert.match(references.content, /UNRESOLVED_PROMPT_REFERENCE/);
assert.match(references.content, /data-reference-selection/);
assert(references.examples.some(({ input }) => input.prompt.includes("<pippit-asset-id>")));

const time = getGuide("time");
assert.match(time.content, /1 秒 = 1000 ms/);
assert.match(time.content, /1 秒 = 1000000 µs/);
assert.match(time.content, /trimStart\/trimEnd 使用源动画秒数/);
assert.deepStrictEqual(time.examples[0].conversion, {
  seconds: 2,
  milliseconds: 2000,
  microseconds: 2000000,
  fps: 30,
  frames: 60,
});

const timeline = getGuide("timeline");
assert.match(timeline.content, /expectedRevision.*draft\.revision/);
assert.match(timeline.content, /frameRate 当前只读/);
assert.match(timeline.content, /真实 Canvas 素材节点/);
assert.strictEqual(timeline.examples[0].input.commands[0].type, "set_output_size");

const scene3d = getGuide("scene3d");
assert.match(scene3d.content, /position 使用米/);
assert.match(scene3d.content, /rotation 为 XYZ Euler 角度/);
assert.match(scene3d.content, /theta\/phi\/arc.*弧度/);
assert.strictEqual(scene3d.examples[0].input.operations[0].args.kind, "camera");
for (const name of ["prompt-references", "timeline", "scene3d"]) {
  assert(
    getGuide(name).examples.every(({ input }) => input.dryRun === true),
    "mutation examples must start with domain dryRun previews",
  );
}

index[0].summary = "changed";
storyboard.content = "changed";
storyboard.examples[0].prompt = "changed";
assert.notStrictEqual(listGuides()[0].summary, "changed");
assert.notStrictEqual(getGuide("storyboard").content, "changed");
assert.notStrictEqual(getGuide("storyboard").examples[0].prompt, "changed");
assert.throws(() => getGuide("unknown"), /canvas command guide/);
assert.throws(() => getGuide("__proto__"), /未知的画布指南/);

console.log("Canvas command guide tests passed (5 topics, budgets, examples and capability boundaries)");

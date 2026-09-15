"use strict";

// Offline usage notes. The installed runtime's list/describe/schema remain the
// source of truth for available commands and their accepted input.
const GUIDES = [
  {
    name: "storyboard",
    summary: "故事板原生脚本标签、时长与引用，以及当前 CLI 的能力边界。",
    content: [
      "故事板脚本保存在镜头资产 content.generation.prompt。现有数据可能是 biz/shot，或 biz/video 且 bizVariant 为 storyboard-shot；故事板 brief 另有镜头列表与顺序。普通 video 节点不等于一个完整故事板镜头。",
      "<duration-ms> 中填写正数毫秒。一个 prompt 中所有有效正数标签累加；有标签总和时优先采用该总和，否则回退到正数 generation.durationMs。空值、非数值或非正数不计入总和。例子中的 2000 + 3000 = 5000 ms。",
      "时间码和起止区间文字只是 prompt 内容，不是可执行的台词调度。标签求和也不保证模型精确按秒完成每个动作或台词。生成会将镜头总毫秒数向上取整为秒，再按当前故事板模型策略校验；可用时长、分辨率组合和最小时长取决于当前模型目录，不能从通用 generate-video 的帮助推断。",
      "node-asset 填真实节点资产 ID，角色形象选择沿用 data-reference-selection 属性。独立媒体可以没有节点，使用 pippit-asset-id，但需已有可解析的引用草稿；只有裸 ID 不代表素材已就绪。先查询真实资产与当前引用，不要从名称猜 ID。",
      "当前没有公开的故事板脚本编辑、镜头增删排序或指定镜头生成领域 command。Canvas 的 xyq.generation.update_prompt 不等同于故事板的脚本保存流程；generate-video 不会自动设置故事板身份或写回指定镜头。通用资产补丁能传输数据，不提供这些业务校验，不能作为故事板领域操作的替代。此处 XML 用于说明既有格式，不是可直接执行的故事板命令。",
      "下一步先用 canvas command list 确认安装版本能力，用 canvas get 读取已知镜头资产；故事板编辑与生成继续使用现有 Web 故事板入口。所有示例 ID 都须替换为当前项目真实 ID。",
    ].join("\n\n"),
    next_commands: [
      "pippit-tool-cli canvas command list",
      "pippit-tool-cli canvas get --asset-id SHOT_ASSET_ID",
      "pippit-tool-cli canvas command guide prompt-references",
      "pippit-tool-cli canvas command guide time",
    ],
    examples: [
      {
        description: "现有故事板 prompt：两个正数毫秒段，总时长 5000 ms；不是 CLI 编辑指令。",
        prompt:
          "<duration-ms>2000</duration-ms><node-asset>ROLE_NODE_ASSET_ID</node-asset>走入雨中的街道。\n<duration-ms>3000</duration-ms><node-asset>ROLE_NODE_ASSET_ID</node-asset>停下脚步，说：我们到了。",
      },
    ],
    related: ["prompt-references", "time"],
  },
  {
    name: "prompt-references",
    summary: "图片/视频 prompt 标签、角色选材与没有节点的独立素材。",
    content: [
      "先用 list 确认 xyq.generation.update_prompt 可用，再 describe 获取参数。该命令接收 nodeId、prompt、可选 dryRun；nodeId 是当前 Canvas 的目标图片或视频节点 ID。get_snapshot 可查节点，get_asset 可读节点资产与 generation.references；用 describe 查看这两个查询命令的输入，避免混淆 Canvas ID、节点 ID 与媒体 Pippit ID。",
      '<node-asset>NODE_ASSET_ID</node-asset> 引用已有节点。标签 ID 来自查询结果中的真实节点资产身份；不要用展示名称代替。CLI 复用 Web 标签匹配、节点连边和角色默认选材，保留标签属性。角色已有 data-reference-selection="IMAGE_ID,VOICE_ID" 属性使用逗号分隔选择 ID，需与角色已有资料匹配，不另传 selectedIds 或 reference 数组。',
      "独立上传/素材库媒体可以没有节点。对已在目标 generation.references 中的素材，用 <pippit-asset-id>MEDIA_PIPPIT_ASSET_ID</pippit-asset-id>，ID 取自该草稿的 pippitAssetId。视频目标可使用已有图片、视频、音频候选；图片目标须符合自身支持的引用类型。无须新增 referenceSource 入参。",
      "只有 canvas upload 返回的 ID 还不够：本命令不会查询未知媒体并加入草稿。canvas get 只查询，不添加引用。找不到的普通标签返回 UNRESOLVED_PROMPT_REFERENCE，不写文档。相同媒体若匹配到已有 Canvas 源节点，沿用 Web 节点优先规则连边。新独立素材先通过现有 Web 上传/素材库流程建立草稿。",
      "dryRun:true 在隔离文档预演，不改变原文档。成功编辑保留其他 generation 参数及 caption/title/name；空 prompt 清空文本并保留现有引用与连边。本命令不触发生成。不要浅覆盖整个 generation。故事板有自己的脚本与引用流程，见 storyboard 指南。",
    ].join("\n\n"),
    next_commands: [
      "pippit-tool-cli canvas command describe xyq.generation.update_prompt",
      "pippit-tool-cli canvas command describe get_snapshot",
      "pippit-tool-cli canvas command describe get_asset",
      "pippit-tool-cli canvas command describe xyq.generation.update_prompt --path properties.prompt",
    ],
    examples: [
      {
        description: "已有 Canvas 节点引用的预演；将两个节点 ID 换成查询到的真实值。",
        command:
          "pippit-tool-cli canvas command run xyq.generation.update_prompt --canvas-id CANVAS_ID --file ./prompt-input.json",
        input: {
          nodeId: "TARGET_IMAGE_NODE_ID",
          prompt: "参考 <node-asset>REFERENCE_NODE_ASSET_ID</node-asset> 的人物，改为雨夜街景。",
          dryRun: true,
        },
      },
      {
        description: "没有节点、但已存在于目标草稿中的独立图片引用。",
        input: {
          nodeId: "TARGET_IMAGE_NODE_ID",
          prompt: "参考 <pippit-asset-id>PIPPIT_ASSET_ID_IN_DRAFT</pippit-asset-id> 的构图。",
          dryRun: true,
        },
      },
    ],
    related: ["storyboard", "time"],
  },
  {
    name: "time",
    summary: "区分故事板毫秒、多轨微秒、3D 帧与源动画秒。",
    content: [
      "各领域单位不同，先 describe 具体字段；不要把一个领域的数值直接复制到另一个领域。此指南只解释单位，不触发写入或生成。",
      "故事板 <duration-ms> 与 generation.durationMs 使用毫秒：1 秒 = 1000 ms。多个正数标签累加；没有有效标签才回退到 generation.durationMs。提交视频时对总毫秒数向上取整为秒，随后遵循当前故事板模型策略。时间码文字不是执行调度，详见 storyboard。",
      "多轨 TimelineDraft 的时间字段使用整数微秒：1 秒 = 1000000 µs。输出 width/height 使用像素。草稿 duration 由片段推导；frameRate 当前只读，没有设置帧率 command。expectedRevision 是草稿版本号，不是时间。",
      "3D 关键帧与动作片段 startFrame/durationFrames 使用帧，timeline.fps 定义帧时长；move_keyframes.deltaFrames 是整数帧偏移。动作片段 trimStart/trimEnd 使用源动画秒数，可带小数，不是帧。",
      "通用 generate-video --duration 使用整数秒，由该视频入口处理；它不会在本地把故事板 duration-ms 标签自动转换成该参数。其模型/时长能力不能替代故事板当前模型约束。",
      "ID 从所属查询取得：Timeline 的轨道/片段/来源 ID 由 xyq.timeline.query 返回；3D 对象/动画 ID 由 xyq.scene3d.query 返回。外层 nodeId 始终从当前 Canvas 节点查询取得。",
    ].join("\n\n"),
    next_commands: [
      "pippit-tool-cli canvas command guide storyboard",
      "pippit-tool-cli canvas command guide timeline",
      "pippit-tool-cli canvas command guide scene3d",
      "pippit-tool-cli canvas command describe xyq.scene3d.apply --operation move_keyframes",
    ],
    examples: [
      {
        description: "仅作换算：在 30 fps 的 3D 时间轴上，持续 2 秒对应 60 帧；不是字段更新请求。",
        conversion: { seconds: 2, milliseconds: 2000, microseconds: 2000000, fps: 30, frames: 60 },
      },
    ],
    related: ["storyboard", "timeline", "scene3d"],
  },
  {
    name: "timeline",
    summary: "查询多轨节点、草稿版本和素材来源，再预演现有编辑命令。",
    content: [
      "多轨的外层 Canvas 节点类型是 biz/timeline-composition，创建工厂 nodeKind 是 timeline-composition。编辑内容位于节点引用的独立 TimelineDraft；create_biz_node 的 initialData 被该工厂忽略，工厂创建空草稿。先 list 确认命令可用，再按 node-kind 查看工厂参数。",
      "已有节点先运行 xyq.timeline.query。nodeId 来自 Canvas 节点查询，expectedRevision 必须使用此次查询的 draft.revision，轨道/片段/来源 ID 使用草稿中的真实 ID。版本不匹配会中止写入；重新查询后基于新状态准备操作。",
      "xyq.timeline.apply.commands 接受现有 Timeline SDK 命令的 {type,payload} 数组。用 describe --operation 按 type 查看单项参数，schema 显式导出完整集合。dryRun:true 执行同一业务校验与原子预演，不保存 Canvas 状态。",
      "时间字段使用整数微秒，1 秒 = 1000000 µs；输出尺寸使用像素。总时长由片段推导，frameRate 当前只读。编辑器会按既有规则闭合主视频轨空隙并删除空副轨，不应假定编辑后草稿只改一处字段。",
      "新增素材需复用已有来源，或关联真实 Canvas 素材节点；不能发明 source ID 或任意 URL 来替代素材解析。渲染、导出、上传与生成需要各自现有运行环境，不包含在草稿编辑命令内。",
    ].join("\n\n"),
    next_commands: [
      "pippit-tool-cli canvas command describe create_biz_node --node-kind timeline-composition",
      'pippit-tool-cli canvas command run xyq.timeline.query --canvas-id CANVAS_ID --input \'{"nodeId":"TIMELINE_NODE_ID"}\'',
      "pippit-tool-cli canvas command describe xyq.timeline.apply --operation set_output_size",
      "pippit-tool-cli canvas command schema xyq.timeline.apply",
    ],
    examples: [
      {
        description: "将查询得到的 draft.revision 替换示例 0，并填写真实 nodeId；先预演输出尺寸。",
        command:
          "pippit-tool-cli canvas command run xyq.timeline.apply --canvas-id CANVAS_ID --file ./timeline-input.json",
        input: {
          nodeId: "TIMELINE_NODE_ID",
          expectedRevision: 0,
          commands: [{ type: "set_output_size", payload: { width: 1920, height: 1080 } }],
          dryRun: true,
        },
      },
    ],
    related: ["time", "scene3d"],
  },
  {
    name: "scene3d",
    summary: "3D 导演文档、对象 ID、空间与动画单位及编辑边界。",
    content: [
      "3D 导演台由外层 Canvas 节点和独立导演文档组成。create_biz_node 使用 nodeKind: scene3d；先 list 确认当前安装版本支持的命令，再 describe --node-kind 查看工厂参数。",
      "xyq.scene3d.query 的 nodeId 是外层 Canvas 节点 ID；返回的对象、摄像机、关键帧等 ID 属于导演文档，不能与 Canvas 节点 ID 混用。先查询文档与可动画属性通道，再引用已有 ID。创建新对象时选文档内未占用的 ID。",
      "xyq.scene3d.apply.operations 是 {command,args} 数组，command 使用现有 Scene3D SDK 命令名。describe --operation 查看一种操作及其 args，schema 可导出完整输入。dryRun:true 预演编辑，不改变原文档。",
      "transform.position 使用米，rotation 为 XYZ Euler 角度，scale 为无单位 XYZ 倍率；camera FOV 使用角度。几何体 theta/phi/arc 等参数使用弧度，以对应 kind 的 schema 为准。",
      "关键帧与 startFrame/durationFrames 使用帧，帧时长取决于 timeline.fps；动作片段 trimStart/trimEnd 使用源动画秒数，可为小数。不要把微秒、毫秒或旋转弧度填入帧或角度字段。",
      "这套命令编辑导演文档；截图、渲染导出、上传与生成需要相应运行环境和现有入口。资源/动作引用应来自已查询到的真实资源，不用临时 URL 或虚构 ID 代替。",
    ].join("\n\n"),
    next_commands: [
      "pippit-tool-cli canvas command describe create_biz_node --node-kind scene3d",
      'pippit-tool-cli canvas command run xyq.scene3d.query --canvas-id CANVAS_ID --input \'{"nodeId":"DIRECTOR_NODE_ID"}\'',
      "pippit-tool-cli canvas command describe xyq.scene3d.apply --operation create_node",
      "pippit-tool-cli canvas command schema xyq.scene3d.apply",
    ],
    examples: [
      {
        description: "填入真实外层 nodeId，并确保 camera-new 在导演文档内未占用，再预演创建摄像机。",
        command:
          "pippit-tool-cli canvas command run xyq.scene3d.apply --canvas-id CANVAS_ID --file ./scene3d-input.json",
        input: {
          nodeId: "DIRECTOR_NODE_ID",
          operations: [{ command: "create_node", args: { kind: "camera", id: "camera-new", name: "Close-up" } }],
          dryRun: true,
        },
      },
    ],
    related: ["time", "timeline"],
  },
];

function listGuides() {
  return GUIDES.map(({ name, summary }) => ({ name, summary }));
}

function getGuide(name) {
  const guide = GUIDES.find((item) => item.name === name);
  if (!guide) {
    throw new Error(`未知的画布指南：${String(name)}。运行 pippit-tool-cli canvas command guide 查看主题。`);
  }
  return JSON.parse(JSON.stringify(guide));
}

module.exports = { listGuides, getGuide };

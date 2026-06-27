#!/usr/bin/env python3
"""创建会话 / 向会话发送消息（生图、生视频等）：POST /api/biz/v1/skill/submit_run"""

import argparse
import json
import sys
import os

sys.path.insert(0, os.path.dirname(__file__))
from xyq_common import submit_run


MODEL_SEEDANCE_2_0_VISION = "seedance2.0_vision"
ALLOWED_VIDEO_MODELS = {
    "Seedance_2.5",
    MODEL_SEEDANCE_2_0_VISION,
    "seedance2.0_fast_vision",
    "Seedance_2.0_mini",
}
RATIO_VALUES = {
    "16:9": 2,
    "9:16": 3,
    "4:3": 4,
    "3:4": 5,
}
RESOLUTION_VALUES = {"480p", "720p", "1080p"}
SUPPORTED_RATIO_NUMBERS = set(RATIO_VALUES.values())
MODEL_BOUND_SETTING_FIELDS = {
    "ratio",
    "duration_start",
    "duration_end",
    "resolution",
}


def fail(message):
    print(f"错误：{message}", file=sys.stderr)
    sys.exit(1)


def parse_general_agent_settings(raw):
    if not raw:
        return None
    try:
        settings = json.loads(raw)
    except json.JSONDecodeError as exc:
        fail(f"--general-agent-settings 不是合法 JSON：{exc}")

    if not isinstance(settings, dict):
        fail("--general-agent-settings 必须是 JSON object")

    settings = dict(settings)
    if has_model_bound_settings(settings) and not has_video_model(settings):
        fail(
            "general_agent_settings 包含 ratio、duration 或 resolution 等模型参数，"
            "但缺少 video_model；请先询问用户使用哪个模型后再提交"
        )
    if has_video_model(settings):
        settings["video_model"] = settings["video_model"].strip()
        validate_video_model(settings["video_model"])
    if "ratio" in settings:
        settings["ratio"] = normalize_ratio(settings["ratio"])
    if "resolution" in settings:
        settings["resolution"] = normalize_resolution(settings["resolution"])
    if "resolution" in settings:
        validate_resolution_for_model(settings)
    if "duration_start" in settings or "duration_end" in settings:
        normalize_duration_range(settings)

    return settings


def validate_video_model(video_model):
    if video_model not in ALLOWED_VIDEO_MODELS:
        fail(
            "video_model 当前只支持 "
            + "、".join(sorted(ALLOWED_VIDEO_MODELS))
        )


def normalize_ratio(value):
    if isinstance(value, bool):
        fail("ratio 当前只支持 9:16、16:9、3:4、4:3")
    if isinstance(value, int):
        if value in SUPPORTED_RATIO_NUMBERS:
            return value
        fail("ratio 当前只支持 9:16、16:9、3:4、4:3")
    if isinstance(value, str):
        ratio = value.strip()
        if ratio in RATIO_VALUES:
            return RATIO_VALUES[ratio]
        fail("ratio 当前只支持 9:16、16:9、3:4、4:3")
    fail("ratio 当前只支持 9:16、16:9、3:4、4:3")


def normalize_resolution(value):
    if not isinstance(value, str):
        fail("resolution 当前只支持 480p、720p、1080p")
    resolution = value.strip().lower()
    if resolution in RESOLUTION_VALUES:
        return resolution
    fail("resolution 当前只支持 480p、720p、1080p")


def validate_resolution_for_model(settings):
    if (
        settings.get("resolution") == "1080p"
        and settings.get("video_model") != MODEL_SEEDANCE_2_0_VISION
    ):
        fail("resolution=1080p 仅支持 video_model=seedance2.0_vision")


def normalize_duration_range(settings):
    if "duration_start" not in settings or "duration_end" not in settings:
        fail("duration_start 和 duration_end 需要成对传入")
    duration_start = normalize_positive_int(settings["duration_start"], "duration_start")
    duration_end = normalize_positive_int(settings["duration_end"], "duration_end")
    if duration_start > duration_end:
        fail("duration_start 不能大于 duration_end")
    settings["duration_start"] = duration_start
    settings["duration_end"] = duration_end


def normalize_positive_int(value, field_name):
    if isinstance(value, bool):
        fail(f"{field_name} 必须是正整数")
    if isinstance(value, int):
        result = value
    elif isinstance(value, float) and value.is_integer():
        result = int(value)
    elif isinstance(value, str) and value.strip().isdigit():
        result = int(value.strip())
    else:
        fail(f"{field_name} 必须是正整数")
    if result <= 0:
        fail(f"{field_name} 必须是正整数")
    return result


def has_video_model(settings):
    video_model = settings.get("video_model")
    return isinstance(video_model, str) and video_model.strip() != ""


def has_model_bound_settings(settings):
    return any(field in settings for field in MODEL_BOUND_SETTING_FIELDS)


def main():
    parser = argparse.ArgumentParser(
        description="创建会话或向已有会话发送消息（仅用于生视频）",
        epilog="""
环境变量:
  XYQ_ACCESS_KEY  必填，Bearer 鉴权
  XYQ_OPENAPI_BASE 或 XYQ_BASE_URL  可选，默认 https://xyq.jianying.com

示例:
  # 创建新会话并发送「生一个动漫视频」
  python3 submit_run.py --message 生一个动漫视频

  # 向已有会话发送消息
  python3 submit_run.py --message 再生成一个动漫视频 --thread-id 90f05e0c-5d08-4148-be40-e30fc7c7bedf

  # 传入文件资产 ID
  python3 submit_run.py --message 生成视频 --asset-ids asset123

  # 传入多个文件资产 ID
  python3 submit_run.py --message 生成视频 --asset-ids asset123 asset456 asset789

  # 传入通用 Agent 设置
  python3 submit_run.py --message 生成视频 --general-agent-settings '{"video_model":"Seedance_2.5","ratio":3,"resolution":"720p"}'
        """,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--message",
        required=True,
        help="要发送的消息内容（生图/生视频描述等），必填",
    )
    parser.add_argument(
        "--thread-id",
        default="",
        help="已有会话 ID，不传则创建新会话或返回已有默认会话",
    )
    parser.add_argument(
        "--asset-ids",
        nargs="+",
        default=[],
        help="资产 ID 列表，可传入多个，例如：--asset-ids id1 id2 id3",
    )
    parser.add_argument(
        "--general-agent-settings",
        default="",
        help='通用 Agent 设置 JSON，例如：{"video_model":"Seedance_2.5","ratio":3,"resolution":"720p"}',
    )
    args = parser.parse_args()
    general_agent_settings = parse_general_agent_settings(args.general_agent_settings)

    data = submit_run(
        thread_id=args.thread_id or "",
        message=args.message or "",
        asset_ids=args.asset_ids if args.asset_ids else None,
        general_agent_settings=general_agent_settings,
    )
    run_data = data.get("run", {})
    web_thread_link = data.get("web_thread_link", "")
    thread_id = run_data.get("thread_id", "")
    run_id = run_data.get("run_id", "")

    if not thread_id:
        print("错误：未返回 thread_id", file=sys.stderr)
        sys.exit(1)
    if not run_id:
        print("错误：未返回 run_id", file=sys.stderr)
        sys.exit(1)

    out = {"thread_id": thread_id, "run_id": run_id, "web_thread_link": web_thread_link}
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

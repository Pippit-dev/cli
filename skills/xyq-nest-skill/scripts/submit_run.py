#!/usr/bin/env python3
"""创建会话 / 向会话发送消息（生图、生视频等）：POST /api/biz/v1/skill/submit_run"""

import argparse
import json
import sys
import os

sys.path.insert(0, os.path.dirname(__file__))
from _common import submit_run


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
    args = parser.parse_args()

    data = submit_run(
        thread_id=args.thread_id or "",
        message=args.message or "",
        asset_ids=args.asset_ids if args.asset_ids else None
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

#!/usr/bin/env python3
"""查询会话进展：POST /api/biz/v1/skill/get_thread，返回消息列表"""

from _common import extract_entries_from_run
import argparse
import json
import sys
import os

sys.path.insert(0, os.path.dirname(__file__))
from _common import get_thread


def main():
    parser = argparse.ArgumentParser(
        description="查询会话消息列表（会话进展）",
        epilog="""
环境变量:
  XYQ_ACCESS_KEY  必填，Bearer 鉴权
  XYQ_OPENAPI_BASE 或 XYQ_BASE_URL  可选，默认 https://xyq.jianying.com

示例:
  python3 get_thread.py --thread-id abc123 --run-id def456 --after-seq 0
        """,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--thread-id",
        required=True,
        help="会话 ID（由 submit_run 返回）",
    )
    parser.add_argument(
        "--run-id",
        default="",
        help="运行 ID（由 submit_run 返回）",
    )
    parser.add_argument(
        "--after-seq",
        type=int,
        default=0,
        help="只返回 seq 大于等于该值的消息，用于增量拉取（默认 0）",
    )
    args = parser.parse_args()

    run = get_thread(args.thread_id, run_id=args.run_id, after_seq=args.after_seq)
    # 从run中提取Message和Artifact
    entries = extract_entries_from_run(run)
    out = {"messages": entries}
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

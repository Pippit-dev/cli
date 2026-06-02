#!/usr/bin/env python3
"""下载生成结果：从会话中提取所有图片/视频 URL 并批量下载到本地"""

import argparse
import json
import os
import sys
import urllib.request
import urllib.error
from concurrent.futures import ThreadPoolExecutor, as_completed

sys.path.insert(0, os.path.dirname(__file__))


def download_file(url, filepath):
    """下载单个文件"""
    import shutil
    req = urllib.request.Request(url, headers={"User-Agent": "XYQ-Nest-Skill/1.0"})
    tmp_path = filepath + ".tmp"
    try:
        with urllib.request.urlopen(req, timeout=600) as resp:
            with open(tmp_path, "wb") as f:
                shutil.copyfileobj(resp, f, length=1024 * 1024)
        os.replace(tmp_path, filepath)
        return filepath, None
    except Exception as e:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)
        return filepath, str(e)


def main():
    parser = argparse.ArgumentParser(
        description="根据产物URL，下载生成的产物到本地，支持指定输出目录和文件名前缀",
        epilog="""
使用方式:
  # 直接下载指定 URL 列表
  python3 download_results.py --urls URL1 URL2 URL3 --output-dir ./output --prefix "storyboard"
        """,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--urls", nargs="+", required=True, help="直接指定要下载的 URL 列表")
    parser.add_argument("--output-dir", default="", help="输出目录（默认 ./xyq_output")
    parser.add_argument("--prefix", default="", help="文件名前缀（如 'storyboard' → storyboard_01.png）")
    parser.add_argument("--workers", type=int, default=5, help="并行下载线程数（默认 5）")
    args = parser.parse_args()
    
    # 准备输出目录
    output_dir = args.output_dir or "./xyq_output"
    os.makedirs(output_dir, exist_ok=True)

    def _get_ext(url):
        """从 URL 中提取文件扩展名，优先从 query 的 filename 参数取，其次从路径取"""
        from urllib.parse import urlparse, parse_qs
        parsed = urlparse(url)
        qs = parse_qs(parsed.query)
        filenames = qs.get("filename", [])
        if filenames:
            _, ext = os.path.splitext(filenames[0])
            if ext:
                return ext
        _, ext = os.path.splitext(parsed.path)
        return ext or ".bin"

    # 构建下载任务
    tasks = []
    for i, url in enumerate(args.urls, 1):
        ext = _get_ext(url)
        if args.prefix:
            filename = f"{args.prefix}_{i:02d}{ext}"
        else:
            filename = f"{i:02d}{ext}"
        filepath = os.path.join(output_dir, filename)
        tasks.append((url, filepath))

    # 并行下载
    results = []
    errors = []
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {pool.submit(download_file, url, fp): (url, fp) for url, fp in tasks}
        for future in as_completed(futures):
            fp, err = future.result()
            if err:
                errors.append({"file": fp, "error": err})
            else:
                results.append(fp)

    # 按文件名排序输出
    results.sort()

    output = {
        "output_dir": output_dir,
        "downloaded": results,
        "total": len(results),
    }
    if errors:
        output["errors"] = errors

    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""营销 CLI PPE 用例：单次提交、原 Run 查询、服务端路由证据核验；不编译。"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tomllib

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "video"))
from check_billing import BUILD, ROOT, digest, git, read, records, require, source_digest, write

BINARY = BUILD / "pippit-cli-ppe"
SUBMIT_PATH = "/api/biz/v1/agent/submit_marketing_run"


def preflight(env):
    require(re.fullmatch(r"ppe_[A-Za-z0-9_]+", env) and env != "ppe_test", "必须指定专属 PPE 泳道")
    with (ROOT / "config.toml").open("rb") as stream:
        config = tomllib.load(stream)
    require(config.get("x-tt-env", "").strip() == env, "config.toml 的 x-tt-env 与 --env 不一致")
    require(config.get("xyq_api_url", "").strip().rstrip("/") == "https://xyq.jianying.com", "本用例仅验证 xyq API")
    build = read(BUILD / "build.json")
    require(build["source_sha256"] == source_digest() and build["binary_sha256"] == digest(BINARY),
            "测试二进制与当前源码不一致；需另获授权后重新构建既有测试入口")
    return {"env": env, "head": git("rev-parse", "HEAD"), "branch": git("branch", "--show-current"), "build": build}


def cli(out, name, args, body=None, expected_error=None):
    # 仅传给既有测试入口作为证据目录；此环境变量不注入 PPE header。
    env = dict(os.environ, VIDEO_TEST_OUTPUT=str(out), PIPPIT_CLI_DISABLE_UPDATE_CHECK="1", PIPPIT_CLI_DISABLE_TELEMETRY="1")
    with (out / f"{name}.json").open("x") as stdout, (out / f"{name}.stderr").open("x") as stderr:
        result = subprocess.run([str(BINARY), *args], cwd=ROOT, env=env,
                                input=None if body is None else json.dumps(body), text=True, stdout=stdout, stderr=stderr)
    if expected_error:
        require(result.returncode != 0 and expected_error in (out / f"{name}.stderr").read_text(), f"{name} 未按预期拒绝")
        return
    require(result.returncode == 0, f"{name} 失败，检查 {out / (name + '.stderr')}；禁止自动重提生成")
    return read(out / f"{name}.json")


def business_ok(response):
    require(response.get("ret") in (0, "0") and isinstance(response.get("data"), dict), "API 业务失败或缺少 data")
    require(bool(response.get("log_id")), "缺少用于验证 PPE 的 log_id")
    return response["data"]


def submit(args):
    metadata = preflight(args.env)
    body = read(args.request)
    require(not body.get("thread_id"), "首次测试不能带 thread_id；已有任务应 query，不重新创建")
    out = Path(args.output).resolve()
    out.mkdir(parents=True, exist_ok=False, mode=0o700)
    roi = read(args.roi) if args.roi else None
    if args.roi:
        require(isinstance(roi, dict) and all(isinstance(v, str) for v in roi.values()), "ROI fixture 必须为字符串对象")
        require(set(roi) <= {"scene_lv1", "scene_lv2", "tool_id", "tab_name", "edit_type", "enter_from"}, "ROI fixture 仅允许业务标签")
        require(all(roi.get(k, "").strip() for k in ("scene_lv1", "scene_lv2", "tool_id")), "ROI fixture 缺少必需业务标签")
        write(out / "roi.json", roi)
    write(out / "case.json", {**metadata, "started_at": datetime.now(timezone.utc).isoformat(),
                             "request": body, "source": args.source,
                             "test_only_roi": roi,
                             "upload_file": str(Path(args.file).resolve()) if args.file else None})
    source = ["--source", args.source]
    preview_args = ["marketing", "generate", "--request", "-", *source]
    preview = cli(out, "preview", preview_args, body)
    expected_body = {**body, **({"platform": args.source.strip()} if args.source.strip() else {})}
    require(preview.get("dry_run") is True and preview.get("body") == expected_body and
            preview.get("url") == "https://xyq.jianying.com" + SUBMIT_PATH, "预览改变了请求内容或目标接口")
    cli(out, "reject-no-model", preview_args, {"message": "参数校验", "general_agent_settings": {}}, "video_model 必填")
    cli(out, "reject-extra-field", preview_args, {**body, "mode": "unsupported"}, "未支持的营销请求字段")
    cli(out, "reject-query-ids", ["marketing", "query"], expected_error="query 需要 --thread-id 和 --run-id")
    status = cli(out, "status", ["status"])
    require(status.get("logged_in") is True, "CLI 未登录，请使用已有 login 流程，不提供密钥")
    balance = business_ok(cli(out, "balance-before", ["marketing", "balance"]))
    require(isinstance(balance.get("total_remain_amount"), str) and balance["total_remain_amount"].isdigit(), "余额格式异常")
    if args.file:
        upload = business_ok(cli(out, "upload", ["marketing", "upload", "--file", str(Path(args.file).resolve())]))
        require(isinstance(upload.get("pippit_asset_id"), str) and upload["pippit_asset_id"], "缺少上传资产 ID")
        body["asset_ids"] = [*body.get("asset_ids", []), upload["pippit_asset_id"]]
    write(out / "request.json", body)
    cli(out, "preview-uploaded", preview_args, body)
    # 发送前落盘；失败或超时也不能复用此目录重新提交。
    write(out / "submit-attempt.json", {"time": datetime.now(timezone.utc).isoformat(), "path": SUBMIT_PATH})
    response = cli(out, "submit", [*preview_args, "--execute"], body)
    if roi is not None:
        captured = read(out / "roi-request.json")
        require(captured["path"] == SUBMIT_PATH and json.loads(captured["babi_param"]) == roi, "实际 ROI query 与 fixture 不一致")
    run = business_ok(response).get("run", {})
    require(run.get("thread_id") and run.get("run_id"), "提交结果缺少 ID，需核查，禁止自动重提")
    return {"status": "SUBMITTED_NOT_VERIFIED", "evidence": str(out), "log_id": response["log_id"],
            "thread_id": run["thread_id"], "run_id": run["run_id"], "web_thread_link": response["data"].get("web_thread_link")}


def query(args):
    out = Path(args.output).resolve()
    preflight(read(out / "case.json")["env"])
    # 轮询及状态含义全部复用 Skill；只建立一个有时限的等待进程。
    return subprocess.call(["node", str(Path(__file__).with_name("query.js")), str(out), str(args.max_wait)], cwd=ROOT,
                           env=dict(os.environ, VIDEO_TEST_OUTPUT=str(out), PIPPIT_CLI_DISABLE_UPDATE_CHECK="1", PIPPIT_CLI_DISABLE_TELEMETRY="1"))


def verify(out, log_paths):
    case, response = read(out / "case.json"), read(out / "submit.json")
    run = business_ok(response)["run"]
    entries = [record for log_path in log_paths for record in records(read(log_path))]
    scoped = [r for r in entries if r.get("env") == case["env"]]
    api_results = {}
    header_results = {}
    observed_envs = {}
    for name in ("balance-before", "upload", "submit", "query-result"):
        if name == "upload" and not case.get("upload_file"):
            continue
        result = read(out / f"{name}.json")
        business_ok(result)
        api_rows = [r for r in entries if r.get("psm") == "pippit.business.api" and r.get("logid") == result["log_id"]]
        observed_envs[name] = sorted({r["env"] for r in api_rows if r.get("env")})
        api_results[name] = observed_envs[name] == [case["env"]]
        for row in api_rows:
            if "[header] " in row["message"]:
                headers, _ = json.JSONDecoder().raw_decode(row["message"].split("[header] ", 1)[1])
                headers = {key.lower(): value for key, value in headers.items()}
                if headers.get("x-tt-env") == case["env"] and headers.get("x-use-ppe") == "1":
                    header_results[name] = True
    agent = any(r.get("psm") == "pippit.business.agent" and r.get("logid") == response["log_id"] and
                run["thread_id"] in r["message"] and run["run_id"] in r["message"] for r in scoped)
    result = read(out / "query-result.json")
    data = business_ok(result)
    complete = data.get("thread_id") == run["thread_id"] and data.get("run_id") == run["run_id"] and str(data.get("run_state")) == "3"
    complete = complete and bool(data.get("video_urls"))
    roi = case.get("test_only_roi")
    roi_matches = None
    if roi is not None:
        captured = read(out / "roi-request.json")
        roi_matches = captured.get("path") == SUBMIT_PATH and json.loads(captured.get("babi_param", "null")) == roi
    passed = all(api_results.values()) and agent and complete and all(header_results.get(name) for name in api_results) and roi_matches is not False
    return {"status": "MARKETING_PPE_CHAIN_VERIFIED" if passed else "MARKETING_PPE_CHAIN_NOT_VERIFIED",
            "env": case["env"], "api_ppe": api_results, "api_ppe_headers": header_results, "api_observed_envs": observed_envs,
            "agent_ppe": agent, "run_id": run["run_id"], "thread_id": run["thread_id"],
            "log_id": response["log_id"], "run_state": data.get("run_state"), "fail_reason": data.get("fail_reason"),
            "completed_with_video": complete, "video_count": len(data.get("video_urls") or []),
            "test_only_roi": roi, "roi_request_matches_fixture": roi_matches,
            "limits": "验证实际路由和本例产物；不代表所有营销类型、模型、团队隔离或最终计费核销均已验证"}


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    start = commands.add_parser("submit")
    start.add_argument("--env", required=True)
    start.add_argument("--output", required=True)
    start.add_argument("--request", required=True)
    start.add_argument("--file", help="可选的已授权本地素材；上传所得 ID 用于本次生成")
    start.add_argument("--roi", help="仅测试入口注入的 babi_param JSON；不传则保留正式 CLI 行为")
    start.add_argument("--source", default="", help="真实宿主标识；未知则留空")
    poll = commands.add_parser("query")
    poll.add_argument("--output", required=True)
    poll.add_argument("--max-wait", type=int, default=900)
    proof = commands.add_parser("verify")
    proof.add_argument("--output", required=True)
    proof.add_argument("--logs", nargs="+", required=True, help="带 _env/_psm/__logid 的 bytedcli 结构化日志 JSON")
    args = parser.parse_args()
    if args.action == "submit":
        result = submit(args)
    elif args.action == "query":
        return query(args)
    else:
        out = Path(args.output).resolve()
        result = verify(out, args.logs)
        with (out / "verification-history.jsonl").open("a") as stream:
            stream.write(json.dumps({"time": datetime.now(timezone.utc).isoformat(), **result}, ensure_ascii=False) + "\n")
        (out / "verification.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 1 if result["status"] == "MARKETING_PPE_CHAIN_NOT_VERIFIED" else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)

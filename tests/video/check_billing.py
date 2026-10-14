#!/usr/bin/env python3
"""本地 CLI 分支的 Video Part PPE 计费检查；各阶段显式执行，不自动重交。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]
BUILD = ROOT / ".local/tests/video"
SCENE = "cli_turbo_video_generator"
AGENT = "pippit_video_part_agent"


def read(path):
    return json.loads(Path(path).read_text())


def write(path, value):
    with Path(path).open("x", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    Path(path).chmod(0o600)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def source_digest():
    names = git("ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go", "go.mod", "go.sum").splitlines()
    data = "\n".join(f"{name}:{digest(ROOT / name)}" for name in sorted(set(names)) if (ROOT / name).is_file())
    return hashlib.sha256(data.encode()).hexdigest()


def build(_args):
    BUILD.mkdir(parents=True, exist_ok=True)
    binary = BUILD / "pippit-cli-ppe"
    before = source_digest()
    subprocess.run(["go", "build", "-o", str(binary), "./tests/video/cli"], cwd=ROOT, check=True)
    require(before == source_digest(), "编译期间源码发生变化，请重新确认后构建")
    metadata = {"head": git("rev-parse", "HEAD"), "branch": git("branch", "--show-current"),
                "source_sha256": before, "binary_sha256": digest(binary)}
    path = BUILD / "build.json"
    path.write_text(json.dumps(metadata, indent=2) + "\n")
    path.chmod(0o600)
    return {"status": "BUILT", "binary": str(binary), **metadata}


def tuple_value(value):
    require(isinstance(value, dict), "三元组缺失")
    # Agent 的 BenefitTriplet 使用历史拼写 benifit_type；TCC V2 使用 benefit_type。
    result = {key: value.get(key) for key in ("resource_type", "resource_id")}
    result["benefit_type"] = value.get("benefit_type", value.get("benifit_type"))
    require(all(isinstance(v, str) and v.strip() for v in result.values()), "三元组字段不完整")
    return {key: value.strip() for key, value in result.items()}


def select_tuple(root, scene, model, resolution):
    entries = root["scene_config_map"][scene]["client_config"]["models"]
    matches = [item for item in entries if item["key"].lower() == model.lower()]
    require(len(matches) == 1, f"{scene} 中模型 {model} 缺失或重复")
    config = matches[0]["parameter_config"]["commerce_config_v2"]
    facts = {"resolution": resolution.lower(), "reference_mode": "none"}
    for rule in config.get("rules", []):
        require(not rule.get("numeric_conditions"), "此脚本仅覆盖普通文生视频，不能解释数值条件")
        conditions = rule.get("conditions", {})
        require(set(conditions) <= set(facts), "计费规则存在当前测试未提供的条件")
        if all(facts[key] == value.strip().lower() for key, value in conditions.items()):
            return tuple_value(rule.get("benefit_tuple"))
    return tuple_value(config.get("fallback_benefit_tuple"))


def preflight(args):
    require(args.env.startswith("ppe_") and args.env != "ppe_test", "必须指定专属 PPE 泳道")
    draft = getattr(args, "draft", False)
    require((args.model == "Seedance_2.5_draft") == draft, "Draft 预览必须同时指定 Seedance_2.5_draft 和 --draft")
    require(not draft or args.resolution == "480p", "Draft 预览实际计费规格固定为 480p")
    data = read(args.tcc_response)["data"]
    require(data["env"] == args.env and data["region"] == "SINFONLINE", "TCC 快照环境或 region 不匹配")
    require(data["conf_name"] == "available_model_list_config" and data["dir_info"]["dir_path"] == "/default", "TCC key 或目录不匹配")
    online = data.get("online_version") or {}
    require(online.get("version") and online.get("base"), "PPE TCC 尚无 online 配置，不能使用 latest 草稿作为生效证据")
    root = json.loads(online["base"])
    expected = select_tuple(root, SCENE, args.model, args.resolution)
    original = select_tuple(root, "web_turbo_video_generator", args.model, args.resolution)
    require(not getattr(args, "require_web_equal", False) or expected == original, "CLI 三元组与 Web 不一致")
    return {"status": "READY", "env": args.env, "scene": SCENE, "model": args.model,
            "resolution": args.resolution.lower(), "reference_mode": "none",
            "draft": draft, "require_web_equal": getattr(args, "require_web_equal", False),
            "online_version": online["version"], "tcc_sha256": digest(args.tcc_response),
            "expected_tuple": expected, "web_tuple": original,
            "tuple_differs_from_web": expected != original}


def submit(args):
    expected = preflight(args)
    binary = BUILD / "pippit-cli-ppe"
    metadata = read(BUILD / "build.json")
    require(metadata["source_sha256"] == source_digest() and metadata["binary_sha256"] == digest(binary), "测试二进制与当前源码不一致，需显式重新构建")
    out = Path(args.output).resolve()
    out.mkdir(parents=True, exist_ok=False, mode=0o700)
    env = dict(os.environ, VIDEO_TEST_PPE_ENV=args.env, VIDEO_TEST_OUTPUT=str(out))
    status = subprocess.run([str(binary), "status"], cwd=ROOT, env=env, capture_output=True, text=True, check=True)
    require(json.loads(status.stdout).get("logged_in"), "本机 CLI 未登录；先使用正式 CLI 登录")
    command = [str(binary), "generate-video", "--model", args.model, "--resolution", args.resolution,
               "--duration", str(args.duration), "--ratio", args.ratio, "--prompt", args.prompt, "--source", args.source]
    if args.draft:
        command.append("--draft")
    write(out / "case.json", {**expected, "build": metadata, "command": command,
                             "started_at": datetime.now(timezone.utc).isoformat()})
    with (out / "cli.stdout").open("x") as stdout, (out / "cli.stderr").open("x") as stderr:
        result = subprocess.run(command, cwd=ROOT, env=env, stdout=stdout, stderr=stderr)
    require((out / "response.json").exists(), f"未取得响应证据，禁止重交；检查 {out}")
    response = read(out / "response.json")
    require(result.returncode == 0 and response["ret"] == "0" and response["run_id"] and response["log_id"], f"提交未确认成功，禁止重交；检查 {out}")
    return {"status": "SUBMITTED_NOT_VERIFIED", "evidence": str(out), **response}


def records(value, context=None):
    context = dict(context or {})
    if isinstance(value, dict):
        # bytedcli rolling 的一条日志由 messages[{key, value}] 表示。
        if isinstance(value.get("messages"), list):
            fields = {item["key"]: item.get("value") for item in value["messages"]
                      if isinstance(item, dict) and isinstance(item.get("key"), str)}
            yield from records(fields, context)
        for key in ("_env", "env", "_psm", "psm", "__logid", "_logid", "logid", "log_id"):
            if isinstance(value.get(key), str):
                context[key.lstrip("_").replace("log_id", "logid")] = value[key]
        for key in ("_msg", "msg", "message", "content"):
            if isinstance(value.get(key), str):
                yield {**context, "message": value[key]}
        for child in value.values():
            if isinstance(child, (dict, list)):
                yield from records(child, context)
    elif isinstance(value, list):
        for child in value:
            yield from records(child, context)


def after(text, marker):
    return json.JSONDecoder().raw_decode(text.split(marker, 1)[1].lstrip())[0]


def verify(args):
    out = Path(args.output)
    case, response = read(out / "case.json"), read(out / "response.json")
    raw_logs = read(args.logs)
    logs = [entry for entry in records(raw_logs) if entry.get("env") == case["env"]
            and entry.get("psm") == "pippit.business.agent" and entry.get("logid") == response["log_id"]]
    require(logs, "没有同一 LogID、目标 PPE 和 Agent PSM 的日志；不能把请求头当成路由成功证据")
    resolved, deducted, histories, deduction_runs = False, False, [], set()
    quota_counts, credit_amounts = set(), set()
    for entry in logs:
        line = entry["message"]
        if "[CommercialUsage][Resolve] status=success" in line:
            request = after(line, "req=")
            triplet = tuple_value(after(line, "benefit_triplet="))
            resolved |= (request.get("scene") == SCENE and request.get("model_key", "").lower() == case["model"].lower()
                         and request.get("parameter_values", {}).get("resolution") == case["resolution"]
                         and request.get("reference_mode") == "none" and triplet == case["expected_tuple"])
        if "[CheckDeductBenefit] check deduct benefit req:" in line:
            request = after(line, "req:")
            deduction_runs.add(request.get("RunID"))
            if request.get("RunID") == response["run_id"] and request.get("ThreadID") == response["thread_id"]:
                require(request.get("AgentName") == AGENT, "预扣 Agent 与 Video Part 不符")
                require(tuple_value(request.get("BenefitTriplet")) == case["expected_tuple"], "预扣三元组与 CLI 配置不符")
                if "UsedQuotaCount" in request:
                    quota_counts.add(request["UsedQuotaCount"])
                deducted = True
        if "[CheckDeductBenefit] check deduct benefit resp:" in line:
            result = after(line, "resp:")
            # faceu_commerce_consumption.CheckDeductStatus_Success = 1。
            if isinstance(result, dict) and result.get("Status") == 1 and result.get("HistoryID"):
                histories.append(result["HistoryID"])
                if "CreditAmount" in result:
                    credit_amounts.add(result["CreditAmount"])
    require(resolved, "缺少匹配 CLI scene 和三元组的 Resolve 成功日志")
    require(deducted and deduction_runs == {response["run_id"]} and len(set(histories)) == 1,
            "缺少当前 Run 的预扣请求或唯一 HistoryID，或日志混入其他 Run；需补充同次执行证据")
    require(not case.get("require_web_equal") or case["expected_tuple"] == case["web_tuple"], "预扣三元组与 Web 不一致")
    report = {"status": "BILLING_SWITCH_VERIFIED", "env": case["env"], "scene": SCENE,
              "run_id": response["run_id"], "log_id": response["log_id"], "history_id": histories[0],
              "benefit_tuple": case["expected_tuple"],
              "tuple_differs_from_web": case.get("tuple_differs_from_web"),
              "used_quota_counts": sorted(quota_counts), "credit_amounts": sorted(credit_amounts),
              "log_retrieval_coverage": raw_logs.get("data", {}).get("retrieval_coverage", {}).get("verdict", "unknown"),
              "generation_and_settlement": "NOT_VERIFIED"}
    # 可以用同一次 Run 的新日志重复核验；不会再次生成视频。
    (out / "verification.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    return report


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    sub.add_parser("build", help="显式编译测试入口，不执行 Go 测试").set_defaults(run=build)
    for action in ("preflight", "submit"):
        command = sub.add_parser(action)
        command.add_argument("--env", required=True)
        command.add_argument("--tcc-response", required=True, help="fetch-tcc-config 保存的 PPE 完整响应")
        command.add_argument("--model", required=True)
        command.add_argument("--resolution", required=True)
        command.add_argument("--draft", action="store_true", help="Seedance_2.5_draft 480p 预览，不包含转成片")
        command.add_argument("--require-web-equal", action="store_true", help="本次配置对齐验收要求 CLI 三元组与 Web 相同")
        command.set_defaults(run=preflight if action == "preflight" else submit)
        if action == "submit":
            command.add_argument("--output", required=True, help="新的私有证据目录，不允许复用")
            command.add_argument("--prompt", required=True)
            command.add_argument("--duration", type=int, required=True)
            command.add_argument("--ratio", required=True)
            command.add_argument("--source", default="", help="真实宿主统计标识；未知时省略")
    command = sub.add_parser("verify")
    command.add_argument("--output", required=True)
    command.add_argument("--logs", required=True, help="bytedcli 的 JSON 日志，需含 env、psm、logid 和 message")
    command.set_defaults(run=verify)
    args = parser.parse_args()
    try:
        print(json.dumps(args.run(args), ensure_ascii=False, indent=2))
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        print(json.dumps({"status": "BLOCKED_OR_FAILED", "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

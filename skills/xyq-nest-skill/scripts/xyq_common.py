"""小云雀 agent-im OpenAPI 公共模块：查询会话（鉴权为 Authorization: Bearer <access_key>）"""

import json
import os
import sys
import urllib.request
import urllib.error
import urllib.parse

# Credentials may only be sent to the fixed production HTTPS origin.
XYQ_BASE = "https://xyq.jianying.com"
ACCESS_KEY = os.environ.get("XYQ_ACCESS_KEY", "")

# API 路径常量
GET_THREAD_PATH = "/api/biz/v1/skill/get_thread"
HTTP_TIMEOUT_SECONDS = 30 * 60

if not ACCESS_KEY:
    print("错误：请设置 XYQ_ACCESS_KEY 环境变量", file=sys.stderr)
    sys.exit(1)


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Never forward credentials or request bodies through a redirect.
        raise urllib.error.HTTPError(req.full_url, code, "API 重定向已拒绝", headers, fp)


def authenticated_open(req):
    target = urllib.parse.urlsplit(req.full_url)
    if (target.scheme != "https" or target.hostname != "xyq.jianying.com"
            or target.port not in (None, 443) or target.username is not None
            or target.password is not None):
        raise urllib.error.URLError("仅允许小云雀生产 HTTPS 地址")
    return urllib.request.build_opener(_NoRedirect()).open(req, timeout=HTTP_TIMEOUT_SECONDS)


def redact_error(value):
    text = str(value)
    return text.replace(ACCESS_KEY, "[REDACTED]") if ACCESS_KEY else text


def _headers():
    return {
        "Authorization": f"Bearer {ACCESS_KEY}",
        "Content-Type": "application/json",
    }


def api_post(path: str, body: dict) -> dict:
    """POST 请求 agent-im OpenAPI"""
    url = f"{XYQ_BASE.rstrip('/')}{path}"
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers=_headers(),
    )
    try:
        with authenticated_open(req) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8") if e.fp else ""
        print(f"API 错误 {e.code}: {redact_error(err_body)}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"网络错误: {redact_error(e.reason)}", file=sys.stderr)
        sys.exit(1)


def api_get(path: str) -> dict:
    """GET 请求 agent-im OpenAPI"""
    url = f"{XYQ_BASE.rstrip('/')}{path}"
    req = urllib.request.Request(url, method="GET", headers=_headers())
    try:
        with authenticated_open(req) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8") if e.fp else ""
        print(f"API 错误 {e.code}: {redact_error(err_body)}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"网络错误: {redact_error(e.reason)}", file=sys.stderr)
        sys.exit(1)


def parse_response(resp: dict) -> dict:
    """
    分析 API 响应。
    响应结构：{"ret":"0","errmsg":"","data":{}}
    如果 ret 不是 "0"，打印错误信息并退出；否则返回 data。
    """
    ret = resp.get("ret", "")
    if ret != "0":
        errmsg = resp.get("errmsg", "未知错误")
        print(f"错误码: {redact_error(ret)}, 错误信息: {redact_error(errmsg)}", file=sys.stderr)
        sys.exit(1)
    return resp.get("data", {})


def get_thread(thread_id: str, run_id: str = "", after_seq: int = 0) -> dict:
    """
    查询会话消息列表。
    返回 data: { messages: [...] }。
    """
    body = {}
    if thread_id:
        body["thread_id"] = thread_id
    if run_id:
        body["run_id"] = run_id
    body["after_seq"] = after_seq
    resp = api_post(GET_THREAD_PATH, body)
    resp = parse_response(resp)
    thread = resp.get("thread", {})
    run_list = thread.get("run_list", [])
    if len(run_list) == 0:
        print("错误：未返回 run_list", file=sys.stderr)
        sys.exit(1)
    run = run_list[0]
    run_state = run.get("state", "")

    # 判断 run_state
    if run_state == 3:
        # 成功
        print("成功：本次创作已完成", file=sys.stderr)
        return run
    elif run_state == 4:
        # 失败
        fail_reason = run.get("fail_reason", "未知失败原因")
        print(f"错误：{redact_error(fail_reason)}", file=sys.stderr)
        sys.exit(1)
    elif run_state == 5:
        # 取消
        print("错误：本次创作已被终止", file=sys.stderr)
        sys.exit(1)
    else:
        print("本次创作进行中", file=sys.stdout)
        return run


def extract_entries_from_run(run: dict) -> list:
    """
    从 Run 的 EntryList 中提取符合条件的 entry。
    """
    matched = []
    for entry in run.get("entry_list") or []:
        e = {}
        message = entry.get("message")
        artifact = entry.get("artifact")
        if message:
            e["id"] = message.get("message_id", "")
            e["role"] = message.get("role", "")
            e["content"] = message.get("content", [])
            client_tool_calls = message.get("client_tool_calls", [])
            if len(client_tool_calls) > 0:
                e["content"].extend(client_tool_calls)  
        if artifact:
            e["id"] = artifact.get("artifact_id", "")
            e["role"] = artifact.get("role", "")
            e["content"] = artifact.get("content", [])
        matched.append(e)
    return matched

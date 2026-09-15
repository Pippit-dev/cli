"""Offline regression checks for the remaining authenticated Python scripts."""

import contextlib
import importlib
import io
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest import mock
import urllib.error
import urllib.request
import urllib.response
from email.message import Message

SCRIPT_DIR = Path(__file__).resolve().parents[1] / "skills/xyq-nest-skill/scripts"
sys.path.insert(0, str(SCRIPT_DIR))
TEST_KEY = "offline-test-access-key"
with mock.patch.dict(os.environ, {
    "XYQ_ACCESS_KEY": TEST_KEY,
    "XYQ_OPENAPI_BASE": "https://untrusted.example",
    "XYQ_BASE_URL": "http://untrusted.example",
}):
    common = importlib.import_module("xyq_common")


class SecurityTests(unittest.TestCase):
    def test_help_and_argument_validation_without_credentials(self):
        env = os.environ.copy()
        env.pop("XYQ_ACCESS_KEY", None)
        for args, exit_code, expected in (
            (["--help"], 0, "--thread-id"),
            ([], 2, "--thread-id"),
            (["--thread-id", "thread_test", "--after-seq", "invalid"], 2, "--after-seq"),
        ):
            with self.subTest(args=args):
                result = subprocess.run(
                    [sys.executable, "-B", str(SCRIPT_DIR / "get_thread.py"), *args],
                    env=env, capture_output=True, text=True, timeout=10,
                )
                self.assertEqual(result.returncode, exit_code, result.stderr)
                self.assertIn(expected, result.stdout + result.stderr)
                self.assertNotIn("错误：请设置 XYQ_ACCESS_KEY", result.stderr)

    def test_missing_credentials_block_requests_before_network(self):
        for method in ("GET", "POST"):
            stderr = io.StringIO()
            with self.subTest(method=method), mock.patch.object(common, "ACCESS_KEY", ""):
                with mock.patch.object(common, "authenticated_open") as send:
                    with contextlib.redirect_stderr(stderr), self.assertRaises(SystemExit) as raised:
                        if method == "GET":
                            common.api_get(common.GET_THREAD_PATH)
                        else:
                            common.get_thread("thread_test")
                    self.assertEqual(raised.exception.code, 1)
                    send.assert_not_called()
                self.assertIn("请设置 XYQ_ACCESS_KEY", stderr.getvalue())

    def test_environment_cannot_change_authenticated_origin(self):
        self.assertEqual(common.XYQ_BASE, "https://xyq.jianying.com")
        with mock.patch.object(common, "authenticated_open", return_value=io.BytesIO(b'{}')) as send:
            common.api_post(common.GET_THREAD_PATH, {"thread_id": "thread_original"})
        request = send.call_args.args[0]
        self.assertEqual(request.full_url, "https://xyq.jianying.com/api/biz/v1/skill/get_thread")
        self.assertEqual(request.get_header("Authorization"), f"Bearer {TEST_KEY}")

    def test_untrusted_targets_rejected_before_network(self):
        for target in (
            "http://xyq.jianying.com/api",
            "https://untrusted.example/api",
            "https://xyq.jianying.com.untrusted.example/api",
            "https://xyq.jianying.com:444/api",
            "https://user@xyq.jianying.com/api",
        ):
            with self.subTest(target=target), mock.patch.object(urllib.request, "build_opener") as build:
                with self.assertRaises(urllib.error.URLError):
                    common.authenticated_open(urllib.request.Request(target))
                build.assert_not_called()

    def test_redirects_never_forward_authorization_or_body(self):
        for status in (301, 302, 303, 307, 308):
            for target in ("https://untrusted.example/steal", "https://xyq.jianying.com/next"):
                requests = []

                class RedirectServer(urllib.request.BaseHandler):
                    handler_order = 100

                    def https_open(self, req):
                        requests.append(req)
                        headers = Message()
                        headers["Location"] = target
                        response = urllib.response.addinfourl(io.BytesIO(b""), headers, req.full_url, status)
                        response.msg = "Redirect"
                        return response

                opener = urllib.request.build_opener(RedirectServer(), common._NoRedirect())
                request = urllib.request.Request(
                    common.XYQ_BASE + common.GET_THREAD_PATH,
                    data=b'{"private":"body"}',
                    headers={"Authorization": f"Bearer {TEST_KEY}"},
                )
                with self.subTest(status=status, target=target):
                    with mock.patch.object(urllib.request, "build_opener", return_value=opener):
                        with self.assertRaises(urllib.error.HTTPError):
                            common.authenticated_open(request)
                    self.assertEqual(len(requests), 1)

    def test_error_response_does_not_echo_key(self):
        for error in (
            urllib.error.HTTPError(common.XYQ_BASE, 500, "failure", {}, io.BytesIO(TEST_KEY.encode())),
            urllib.error.URLError(TEST_KEY),
        ):
            stderr = io.StringIO()
            with mock.patch.object(common, "authenticated_open", side_effect=error):
                with contextlib.redirect_stderr(stderr), self.assertRaises(SystemExit):
                    common.api_post(common.GET_THREAD_PATH, {})
            self.assertNotIn(TEST_KEY, stderr.getvalue())
            self.assertIn("[REDACTED]", stderr.getvalue())

        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr), self.assertRaises(SystemExit):
            common.parse_response({"ret": "1", "errmsg": TEST_KEY})
        self.assertNotIn(TEST_KEY, stderr.getvalue())


if __name__ == "__main__":
    unittest.main()

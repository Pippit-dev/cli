"""使用已构建的原生测试 binary 和本地 HTTP 服务验证测试 Adapter，不编译。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlsplit

from check_marketing import BINARY, BUILD, ROOT, digest, read, source_digest


@unittest.skipUnless(BINARY.exists() and (BUILD / "build.json").exists(), "需已授权构建的测试入口")
class ROIAdapterTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        build = read(BUILD / "build.json")
        if build["source_sha256"] != source_digest() or build["binary_sha256"] != digest(BINARY):
            raise RuntimeError("测试 binary 已过期；须在明确编译授权内重建，不能自动编译")

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.out = Path(self.temp.name)
        self.requests = []
        requests = self.requests

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                requests.append({"path": self.path, "headers": dict(self.headers),
                                 "body": json.loads(self.rfile.read(int(self.headers["Content-Length"])))})
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"ret":"0","data":{}}')

            def log_message(self, *_):
                pass

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        (self.out / "config.toml").write_text(f'xyq_api_url = "http://127.0.0.1:{server.server_port}"\nx-tt-env = "ppe_fixture"\n')
        self.roi = read(ROOT / "tests/marketing/fixtures/roi.json")
        self.body = {"message": "测试", "general_agent_settings": {"video_model": "Seedance_2.5", "show_subtitle": False}}

    def invoke(self, action="generate"):
        args = [str(BINARY), "marketing", action]
        if action == "generate":
            args += ["--request", "-", "--execute", "--source", ""]
        return subprocess.run(args, cwd=self.out, text=True, input=json.dumps(self.body), capture_output=True, timeout=10,
                              env=dict(os.environ, VIDEO_TEST_OUTPUT=str(self.out), XYQ_ACCESS_KEY="fixture-key",
                                       PIPPIT_CLI_DISABLE_UPDATE_CHECK="1", PIPPIT_CLI_DISABLE_TELEMETRY="1"))

    def fixture(self, value):
        (self.out / "roi.json").write_text(json.dumps(value))

    def test_query_only_and_preserve_body_auth_ppe(self):
        self.fixture(self.roi)
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.requests), 1)
        request = self.requests[0]
        self.assertEqual(json.loads(parse_qs(urlsplit(request["path"]).query)["babi_param"][0]), self.roi)
        self.assertEqual(request["body"], self.body)
        headers = {k.lower(): v for k, v in request["headers"].items()}
        self.assertEqual(headers["authorization"], "Bearer fixture-key")
        self.assertEqual(headers["x-tt-env"], "ppe_fixture")
        self.assertEqual(headers["x-use-ppe"], "1")
        self.assertEqual(json.loads(read(self.out / "roi-request.json")["babi_param"]), self.roi)

    def test_absent_fixture_preserves_original_request(self):
        self.assertEqual(self.invoke().returncode, 0)
        self.assertEqual(urlsplit(self.requests[0]["path"]).query, "")

    def test_balance_does_not_receive_roi(self):
        self.fixture(self.roi)
        self.assertEqual(self.invoke("balance").returncode, 0)
        self.assertEqual(urlsplit(self.requests[0]["path"]).query, "")

    def test_duplicate_submit_is_stopped(self):
        self.fixture(self.roi)
        self.assertEqual(self.invoke().returncode, 0)
        self.assertNotEqual(self.invoke().returncode, 0)
        self.assertEqual(len(self.requests), 1)

    def test_reject_invalid_fixture_before_network(self):
        for value in (None, {}, {**self.roi, "tool_id": 3}, {**self.roi, "user_id": "forged"}):
            with self.subTest(value=value):
                self.fixture(value)
                self.assertNotEqual(self.invoke().returncode, 0)
                self.assertEqual(self.requests, [])


if __name__ == "__main__":
    unittest.main()

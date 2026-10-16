"""离线反例防止把错误泳道、其他 Run 或仅提交成功误报为链路通过。"""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_marketing as check


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.out = Path(self.temp.name)
        self.ids = {"thread_id": "marketing-thread", "run_id": "marketing-run"}
        self.save("case", {"env": "ppe_case", "upload_file": "fixture.mp4"})
        self.logs = []
        for name in ("balance-before", "upload", "submit", "query-result"):
            data = {"run": self.ids} if name == "submit" else {**self.ids, "run_state": "3", "video_urls": ["https://example.invalid/video"]}
            self.save(name, {"ret": "0", "log_id": name, "data": data})
            self.logs.append({"_env": "ppe_case", "_psm": "pippit.business.api", "__logid": name,
                              "_msg": '[header] {"X-Tt-Env":"ppe_case","X-Use-Ppe":"1"} || [req] {}'})
        self.logs.append({"_env": "ppe_case", "_psm": "pippit.business.agent", "__logid": "submit", "_msg": "marketing-thread marketing-run"})

    def save(self, name, value):
        (self.out / f"{name}.json").write_text(json.dumps(value))

    def verify(self):
        self.save("logs", {"content": [{"messages": [{"key": k, "value": v} for k, v in r.items()]} for r in self.logs]})
        return check.verify(self.out, [self.out / "logs.json"])

    def test_exact_evidence(self):
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_VERIFIED")

    def test_wrong_environment(self):
        self.logs[-1]["_env"] = "prod"
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_wrong_run(self):
        self.logs[-1]["_msg"] = "marketing-thread other-run"
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_missing_api_evidence(self):
        self.logs.pop(0)
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_missing_ppe_header(self):
        self.logs[0]["_msg"] = '[header] {"X-Tt-Env":"ppe_case"}'
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_ppe_header_does_not_prove_ppe_routing(self):
        self.logs[3]["_env"] = "prod"
        proof = self.verify()
        self.assertTrue(proof["api_ppe_headers"]["query-result"])
        self.assertFalse(proof["api_ppe"]["query-result"])
        self.assertEqual(proof["api_observed_envs"]["query-result"], ["prod"])
        self.assertEqual(proof["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_working_and_interaction_are_not_completed(self):
        for state in ("2", "6", "9", "99"):
            with self.subTest(state=state):
                self.save("query-result", {"ret": 0, "log_id": "query-result", "data": {**self.ids, "run_state": state}})
                self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_completed_without_video(self):
        self.save("query-result", {"ret": 0, "log_id": "query-result", "data": {**self.ids, "run_state": "3"}})
        self.assertEqual(self.verify()["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")

    def test_failed_null_media_preserves_reason(self):
        reason = {"code": 11004, "message": "服务器繁忙"}
        self.save("query-result", {"ret": 0, "log_id": "query-result", "data": {**self.ids, "run_state": 4, "video_urls": None, "fail_reason": reason}})
        proof = self.verify()
        self.assertEqual(proof["status"], "MARKETING_PPE_CHAIN_NOT_VERIFIED")
        self.assertEqual(proof["fail_reason"], reason)

    def test_reject_environment_mismatch_before_invocation(self):
        with patch.object(check, "ROOT", self.out):
            (self.out / "config.toml").write_text('x-tt-env = "ppe_other"\n')
            with self.assertRaisesRegex(ValueError, "不一致"):
                check.preflight("ppe_case")

    def test_existing_directory_prevents_submission(self):
        args = type("Args", (), {"env": "ppe_case", "request": self.out / "case.json", "output": str(self.out)})()
        with patch.object(check, "preflight", return_value={}), patch.object(check, "cli") as invoke:
            with self.assertRaises(FileExistsError):
                check.submit(args)
            invoke.assert_not_called()


if __name__ == "__main__":
    unittest.main()

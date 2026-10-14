"""离线检查证据解析；不调用 CLI、网络、编译或真实生成。"""
import argparse
import json
from pathlib import Path
import tempfile
import unittest

import check_billing as billing


CLI = {"resource_type": "fixture_type", "resource_id": "fixture_cli", "benefit_type": "fixture_benefit"}
WEB = {**CLI, "resource_id": "fixture_web"}
ENV = "ppe_fixture"


class BillingEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.out = Path(self.tmp.name)
        self.case = {"env": ENV, "model": "Seedance_2.5", "resolution": "720p", "expected_tuple": CLI}
        self.response = {"log_id": "fixture-log", "run_id": "fixture-run", "thread_id": "fixture-thread"}
        self.save("case.json", self.case)
        self.save("response.json", self.response)
        triplet = {"resource_type": CLI["resource_type"], "resource_id": CLI["resource_id"], "benifit_type": CLI["benefit_type"]}
        request = {"scene": billing.SCENE, "model_key": self.case["model"], "parameter_values": {"resolution": "720p"}, "reference_mode": "none"}
        deduct = {"RunID": "fixture-run", "ThreadID": "fixture-thread", "AgentName": billing.AGENT, "BenefitTriplet": triplet}
        self.messages = [
            "[CommercialUsage][Resolve] status=success req=" + json.dumps(request) + " benefit_triplet=" + json.dumps(triplet),
            "[CheckDeductBenefit] check deduct benefit req: " + json.dumps(deduct),
            '[CheckDeductBenefit] check deduct benefit resp: {"Status":1,"HistoryID":"fixture-history"}',
        ]

    def save(self, name, value):
        (self.out / name).write_text(json.dumps(value))

    def verify(self, messages=None, env=ENV):
        entries = [{"_env": env, "_psm": "pippit.business.agent", "_logid": "fixture-log", "_msg": message}
                   for message in (self.messages if messages is None else messages)]
        self.save("logs.json", {"status": "success", "data": {"logs": entries}})
        return billing.verify(argparse.Namespace(output=self.out, logs=self.out / "logs.json"))

    def test_matching_evidence(self):
        self.assertEqual(self.verify()["status"], "BILLING_SWITCH_VERIFIED")

    def test_rolling_key_value_logs(self):
        entries = [{"messages": [{"key": key, "value": value} for key, value in {
            "_env": ENV, "_psm": "pippit.business.agent", "__logid": "fixture-log", "_msg": message}.items()]}
                   for message in self.messages]
        self.save("logs.json", {"data": {"data": {"content": entries}, "retrieval_coverage": {"verdict": "partial"}}})
        result = billing.verify(argparse.Namespace(output=self.out, logs=self.out / "logs.json"))
        self.assertEqual(result["status"], "BILLING_SWITCH_VERIFIED")
        self.assertEqual(result["log_retrieval_coverage"], "partial")

    def test_text_logs_without_environment_are_not_success(self):
        self.save("logs.json", {"data": {"logid": "fixture-log", "data": {"logs": self.messages}}})
        with self.assertRaisesRegex(ValueError, "目标 PPE"):
            billing.verify(argparse.Namespace(output=self.out, logs=self.out / "logs.json"))

    def test_wrong_route_is_not_success(self):
        with self.assertRaises(ValueError):
            self.verify(env="prod")

    def test_web_scene_with_same_tuple_is_not_success(self):
        with self.assertRaisesRegex(ValueError, "CLI scene"):
            self.verify([line.replace(billing.SCENE, "web_turbo_video_generator") for line in self.messages])

    def test_same_tuple_can_verify_independent_scene(self):
        model = {"key": "Seedance_2.5", "parameter_config": {"commerce_config_v2": {"fallback_benefit_tuple": CLI}}}
        root = {"scene_config_map": {scene: {"client_config": {"models": [model]}}
                                     for scene in (billing.SCENE, "web_turbo_video_generator")}}
        self.save("tcc.json", {"data": {"env": ENV, "region": "SINFONLINE", "conf_name": "available_model_list_config",
                                      "dir_info": {"dir_path": "/default"},
                                      "online_version": {"version": 2, "base": json.dumps(root)}}})
        case = billing.preflight(argparse.Namespace(env=ENV, model="Seedance_2.5", resolution="720p", tcc_response=self.out / "tcc.json"))
        self.assertFalse(case["tuple_differs_from_web"])
        self.save("case.json", case)
        self.assertEqual(self.verify()["status"], "BILLING_SWITCH_VERIFIED")

    def test_web_tuple_is_not_success(self):
        with self.assertRaises(ValueError):
            self.verify([line.replace("fixture_cli", "fixture_web") for line in self.messages])

    def test_submission_without_billing_is_not_success(self):
        with self.assertRaises(ValueError):
            self.verify([])

    def test_failed_deduction_is_not_success(self):
        with self.assertRaises(ValueError):
            self.verify([line.replace('"Status":1', '"Status":2') for line in self.messages])

    def test_other_run_cannot_supply_history(self):
        with self.assertRaises(ValueError):
            self.verify(self.messages + [self.messages[1].replace("fixture-run", "another-fixture")])

    def test_first_match_and_fallback(self):
        model = {"key": "Seedance_2.5", "parameter_config": {"commerce_config_v2": {
            "rules": [{"conditions": {"resolution": "720p"}, "benefit_tuple": CLI}],
            "fallback_benefit_tuple": WEB}}}
        root = {"scene_config_map": {billing.SCENE: {"client_config": {"models": [model]}}}}
        self.assertEqual(billing.select_tuple(root, billing.SCENE, model["key"], "720p"), CLI)
        self.assertEqual(billing.select_tuple(root, billing.SCENE, model["key"], "480p"), WEB)

    def test_draft_only_is_not_ready(self):
        self.save("tcc.json", {"data": {"env": ENV, "region": "SINFONLINE", "conf_name": "available_model_list_config",
                                      "dir_info": {"dir_path": "/default"}, "online_version": None,
                                      "latest_version": {"version": 1, "base": "{}"}}})
        with self.assertRaisesRegex(ValueError, "online"):
            billing.preflight(argparse.Namespace(env=ENV, model="Seedance_2.5", resolution="720p", tcc_response=self.out / "tcc.json"))

    def test_web_parity_rejects_different_config(self):
        root = {"scene_config_map": {scene: {"client_config": {"models": [{"key": "Seedance_2.5",
                "parameter_config": {"commerce_config_v2": {"fallback_benefit_tuple": triplet}}}]}}
                for scene, triplet in [(billing.SCENE, CLI), ("web_turbo_video_generator", WEB)]}}
        self.save("tcc.json", {"data": {"env": ENV, "region": "SINFONLINE", "conf_name": "available_model_list_config",
                                      "dir_info": {"dir_path": "/default"},
                                      "online_version": {"version": 3, "base": json.dumps(root)}}})
        with self.assertRaisesRegex(ValueError, "Web 不一致"):
            billing.preflight(argparse.Namespace(env=ENV, model="Seedance_2.5", resolution="720p",
                tcc_response=self.out / "tcc.json", require_web_equal=True))

    def test_draft_requires_preview_stage_and_real_resolution(self):
        for draft, resolution in [(False, "480p"), (True, "720p")]:
            with self.subTest(draft=draft, resolution=resolution), self.assertRaisesRegex(ValueError, "Draft"):
                billing.preflight(argparse.Namespace(env=ENV, model="Seedance_2.5_draft", resolution=resolution, draft=draft))


if __name__ == "__main__":
    unittest.main()

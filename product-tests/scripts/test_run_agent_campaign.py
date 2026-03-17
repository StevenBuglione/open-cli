import importlib.util
import json
import pathlib
import tempfile
import unittest


def load_module():
    script_path = pathlib.Path(__file__).with_name("run-agent-campaign.py")
    spec = importlib.util.spec_from_file_location("run_agent_campaign", script_path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class RunAgentCampaignTests(unittest.TestCase):
    def test_print_summary_honors_go_test_failure(self):
        module = load_module()
        rubrics = [{
            "campaign": "mcp-remote-matrix",
            "capability": "mcp-remote",
            "pass": True,
            "criteria": [],
            "knownGaps": [],
        }]
        self.assertEqual(module.print_summary(rubrics, 1), 1)

    def test_exact_test_name_parses_anchored_pattern(self):
        module = load_module()
        self.assertEqual(module.exact_test_name("^TestCampaignMCPRemoteMatrix$"), "TestCampaignMCPRemoteMatrix")
        self.assertIsNone(module.exact_test_name("TestCampaignMCPRemoteMatrix"))

    def test_selected_lane_was_skipped_detects_skip_marker(self):
        module = load_module()
        output = "=== RUN   TestCampaignMCPRemoteMatrix\n--- SKIP: TestCampaignMCPRemoteMatrix (0.00s)\nPASS\n"
        self.assertTrue(module.selected_lane_was_skipped(output, "^TestCampaignMCPRemoteMatrix$"))
        self.assertFalse(module.selected_lane_was_skipped(output, "^TestCampaignMCPStdioMatrix$"))

    def test_print_summary_honors_missing_rubrics_with_failure(self):
        module = load_module()
        self.assertEqual(module.print_summary([], 1), 1)

    def test_write_lane_rubric_preserves_test_recorded_artifacts(self):
        module = load_module()
        lane = {
            "id": "remote-runtime-oauth-client",
            "workstream": "product-validation",
            "capability": "remote-runtime",
            "environmentClass": "ci-containerized",
            "authPattern": "oauthClient",
            "expectedArtifacts": ["rubric.json", "transcript.log", "browser-config.json"],
        }
        rubric = {
            "campaign": "remote-runtime-matrix",
            "artifactPaths": ["browser-config.json"],
        }
        with tempfile.TemporaryDirectory() as tempdir:
            output_dir = pathlib.Path(tempdir)
            transcript = output_dir / "transcript.log"
            transcript.write_text("transcript")
            rubric_path = module.write_lane_rubric(rubric, lane, output_dir, transcript)
            saved = json.loads(rubric_path.read_text())
            self.assertEqual(
                set(saved["artifactPaths"]),
                {"rubric.json", "transcript.log", "browser-config.json"},
            )

    def test_missing_expected_artifacts_detects_missing_files(self):
        module = load_module()
        lane = {"expectedArtifacts": ["rubric.json", "transcript.log", "browser-config.json"]}
        with tempfile.TemporaryDirectory() as tempdir:
            output_dir = pathlib.Path(tempdir)
            (output_dir / "rubric.json").write_text("{}")
            (output_dir / "transcript.log").write_text("ok")
            self.assertEqual(
                module.missing_expected_artifacts(lane, output_dir),
                ["browser-config.json"],
            )


if __name__ == "__main__":
    unittest.main()

import importlib.util
import pathlib
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


if __name__ == "__main__":
    unittest.main()

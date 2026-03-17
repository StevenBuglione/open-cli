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


if __name__ == "__main__":
    unittest.main()

import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import run_conformance


class RunConformanceTests(unittest.TestCase):
    def test_resolve_schema_root_uses_explicit_path(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            schema_root = Path(temp_dir) / "schemas"
            schema_root.mkdir()

            resolved = run_conformance.resolve_schema_root(schema_root)

            self.assertEqual(schema_root, resolved)

    def test_resolve_schema_root_raises_clear_error_when_missing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            missing = Path(temp_dir) / "missing-schemas"
            fake_root = Path(temp_dir) / "repo-root"
            fake_root.mkdir()

            with mock.patch.object(run_conformance, "ROOT", fake_root):
                with self.assertRaisesRegex(FileNotFoundError, "schema root"):
                    run_conformance.resolve_schema_root(missing)


if __name__ == "__main__":
    unittest.main()

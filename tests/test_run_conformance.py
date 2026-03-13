import tempfile
import unittest
from pathlib import Path
from unittest import mock
import json

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

    def test_validate_compatibility_matrix_passes(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir) / "conformance"
            schema_root = Path(temp_dir) / "schemas"
            root.mkdir()
            schema_root.mkdir()

            (root / "README.md").write_text("See COMPATIBILITY.md for the published matrix.\n")
            (root / "COMPATIBILITY.md").write_text("# Compatibility\n")
            (root / "compatibility-matrix.json").write_text(json.dumps({
                "suiteVersion": "1.0.0",
                "specVersion": "0.1.0",
                "publishedAt": "2026-03-13T12:00:00Z",
                "implementations": [
                    {
                        "repo": "https://github.com/example/oas-cli-go",
                        "version": "main",
                        "status": "passing",
                        "features": {
                            "httpCaching": "passing",
                            "refresh": "passing",
                            "observabilityHooks": "passing",
                            "compatibilityMatrix": "passing"
                        }
                    }
                ]
            }))
            (schema_root / "compatibility-matrix.schema.json").write_text(json.dumps({
                "$schema": "https://json-schema.org/draft/2020-12/schema",
                "type": "object",
                "required": ["suiteVersion", "specVersion", "publishedAt", "implementations"]
            }))

            with mock.patch.object(run_conformance, "ROOT", root):
                run_conformance.validate_compatibility_matrix(schema_root)
                run_conformance.validate_docs_linkage()

    def test_validate_compatibility_matrix_fails_when_required_fields_missing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir) / "conformance"
            schema_root = Path(temp_dir) / "schemas"
            root.mkdir()
            schema_root.mkdir()

            (root / "README.md").write_text("See COMPATIBILITY.md for the published matrix.\n")
            (root / "COMPATIBILITY.md").write_text("# Compatibility\n")
            (root / "compatibility-matrix.json").write_text(json.dumps({
                "suiteVersion": "1.0.0"
            }))
            (schema_root / "compatibility-matrix.schema.json").write_text(json.dumps({
                "$schema": "https://json-schema.org/draft/2020-12/schema",
                "type": "object",
                "required": ["suiteVersion", "specVersion", "publishedAt", "implementations"]
            }))

            with mock.patch.object(run_conformance, "ROOT", root):
                with self.assertRaises(SystemExit):
                    run_conformance.validate_compatibility_matrix(schema_root)

    def test_readme_mentions_compatibility_matrix(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir) / "conformance"
            root.mkdir()
            (root / "README.md").write_text("Missing link\n")
            (root / "COMPATIBILITY.md").write_text("# Compatibility\n")

            with mock.patch.object(run_conformance, "ROOT", root):
                with self.assertRaises(SystemExit):
                    run_conformance.validate_docs_linkage()


if __name__ == "__main__":
    unittest.main()

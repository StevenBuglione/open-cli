import tempfile
import unittest
from pathlib import Path
from unittest import mock

from devtools import bootstrap_python_env


class BootstrapPythonEnvTests(unittest.TestCase):
    def test_install_requirements_uses_venv_python_module_pip(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            venv_dir = Path(temp_dir) / ".venv"
            requirements = Path(temp_dir) / "requirements.txt"
            requirements.write_text("jsonschema==4.25.1\n")

            runner = mock.Mock()

            bootstrap_python_env.install_requirements(
                venv_dir=venv_dir,
                requirements_path=requirements,
                runner=runner,
            )

            runner.assert_called_once_with(
                [str(venv_dir / "bin" / "python3"), "-m", "pip", "install", "-q", "-r", str(requirements)],
                check=True,
            )

    def test_requirements_install_is_skipped_when_stamp_matches(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            venv_dir = Path(temp_dir) / ".venv"
            requirements = Path(temp_dir) / "requirements.txt"
            requirements.write_text("PyYAML==6.0.2\n")
            stamp_path = bootstrap_python_env.stamp_path(venv_dir=venv_dir, requirements_path=requirements)
            stamp_path.parent.mkdir(parents=True)
            stamp_path.write_text(
                bootstrap_python_env.build_stamp(requirements_path=requirements),
            )

            runner = mock.Mock()

            bootstrap_python_env.ensure_requirements(
                venv_dir=venv_dir,
                requirements_path=requirements,
                runner=runner,
            )

            runner.assert_not_called()

    def test_distinct_requirements_files_get_distinct_stamp_paths(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            venv_dir = Path(temp_dir) / ".venv"
            spec_requirements = Path(temp_dir) / "spec-requirements.txt"
            conformance_requirements = Path(temp_dir) / "conformance-requirements.txt"

            spec_requirements.write_text("jsonschema==4.25.1\n")
            conformance_requirements.write_text("PyYAML==6.0.2\n")

            self.assertNotEqual(
                bootstrap_python_env.stamp_path(venv_dir=venv_dir, requirements_path=spec_requirements),
                bootstrap_python_env.stamp_path(venv_dir=venv_dir, requirements_path=conformance_requirements),
            )


if __name__ == "__main__":
    unittest.main()

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
                        "repo": "https://github.com/example/open-cli",
                        "version": "main",
                        "status": "passing",
                        "features": {
                            "httpCaching": "passing",
                            "refresh": "passing",
                            "observabilityHooks": "passing",
                            "compatibilityMatrix": "passing",
                            "mcpTransports": "passing",
                            "oauthRuntime": "passing"
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

    def test_validate_fixture_shapes_accepts_mcp_config_examples(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir) / "conformance"
            schema_root = Path(temp_dir) / "schemas"
            (root / "fixtures" / "discovery").mkdir(parents=True)
            (root / "fixtures" / "openapi").mkdir(parents=True)
            (root / "fixtures" / "overlays").mkdir(parents=True)
            (root / "fixtures" / "workflows").mkdir(parents=True)
            (root / "fixtures" / "config").mkdir(parents=True)
            schema_root.mkdir()

            (root / "fixtures" / "discovery" / "api-catalog.linkset.json").write_text("{}")
            (root / "fixtures" / "discovery" / "service-meta.linkset.json").write_text("{}")
            (root / "fixtures" / "openapi" / "tickets.openapi.yaml").write_text("openapi: 3.1.0\ninfo: {title: Tickets, version: '1.0.0'}\npaths: {}\n")
            (root / "fixtures" / "overlays" / "tickets.overlay.yaml").write_text("overlay: 1.0.0\ninfo: {title: Overlay, version: '1.0.0'}\nactions: []\n")
            (root / "fixtures" / "workflows" / "tickets.arazzo.yaml").write_text("arazzo: 1.0.0\ninfo: {title: Workflow, version: '1.0.0'}\nworkflows: []\n")
            (root / "fixtures" / "config" / "project.cli.json").write_text(json.dumps({
                "cli": "1.0.0",
                "mode": {"default": "discover"},
                "sources": {
                    "remoteDocs": {
                        "type": "mcp",
                        "transport": {
                            "type": "streamable-http",
                            "url": "https://mcp.example.com/mcp"
                        }
                    }
                }
            }))
            (root / "fixtures" / "config" / "mcp-compat.cli.json").write_text(json.dumps({
                "cli": "1.0.0",
                "mode": {"default": "discover"},
                "mcpServers": {
                    "filesystem": {
                        "type": "stdio",
                        "command": "npx",
                        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
                    }
                }
            }))
            (schema_root / "cli.schema.json").write_text(json.dumps({
                "$schema": "https://json-schema.org/draft/2020-12/schema",
                "type": "object",
                "required": ["cli", "mode"],
                "$defs": {
                    "secretRef": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["env", "file", "osKeychain", "exec"]},
                            "value": {"type": "string", "minLength": 1}
                        },
                        "additionalProperties": False
                    },
                    "mcpOAuthConfig": {
                        "type": "object",
                        "required": ["mode"],
                        "properties": {
                            "mode": {"const": "clientCredentials"},
                            "tokenURL": {"type": "string", "minLength": 1},
                            "clientId": {"$ref": "#/$defs/secretRef"},
                            "clientSecret": {"$ref": "#/$defs/secretRef"}
                        },
                        "additionalProperties": False
                    },
                    "mcpTransport": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["stdio", "sse", "streamable-http"]},
                            "command": {"type": "string", "minLength": 1},
                            "url": {"type": "string", "minLength": 1}
                        },
                        "allOf": [
                            {
                                "if": {"properties": {"type": {"const": "stdio"}}, "required": ["type"]},
                                "then": {"required": ["command"]}
                            },
                            {
                                "if": {"properties": {"type": {"const": "sse"}}, "required": ["type"]},
                                "then": {"required": ["url"]}
                            },
                            {
                                "if": {"properties": {"type": {"const": "streamable-http"}}, "required": ["type"]},
                                "then": {"required": ["url"]}
                            }
                        ],
                        "additionalProperties": True
                    },
                    "source": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["mcp", "openapi", "serviceRoot", "apiCatalog"]},
                            "transport": {"$ref": "#/$defs/mcpTransport"},
                            "oauth": {"$ref": "#/$defs/mcpOAuthConfig"},
                            "uri": {"type": "string"}
                        },
                        "allOf": [
                            {
                                "if": {"properties": {"type": {"const": "mcp"}}, "required": ["type", "oauth"]},
                                "then": {"properties": {"transport": {"properties": {"type": {"const": "streamable-http"}}, "required": ["type"]}}}
                            }
                        ],
                        "additionalProperties": True
                    }
                },
                "properties": {
                    "cli": {"type": "string"},
                    "mode": {"type": "object"},
                    "sources": {"type": "object", "additionalProperties": {"$ref": "#/$defs/source"}},
                    "mcpServers": {"type": "object"}
                }
            }))

            with mock.patch.object(run_conformance, "ROOT", root):
                run_conformance.validate_fixture_shapes(schema_root)

    def test_validate_fixture_shapes_rejects_invalid_mcp_transport_fixture(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir) / "conformance"
            schema_root = Path(temp_dir) / "schemas"
            (root / "fixtures" / "discovery").mkdir(parents=True)
            (root / "fixtures" / "openapi").mkdir(parents=True)
            (root / "fixtures" / "overlays").mkdir(parents=True)
            (root / "fixtures" / "workflows").mkdir(parents=True)
            (root / "fixtures" / "config").mkdir(parents=True)
            schema_root.mkdir()

            (root / "fixtures" / "discovery" / "api-catalog.linkset.json").write_text("{}")
            (root / "fixtures" / "discovery" / "service-meta.linkset.json").write_text("{}")
            (root / "fixtures" / "openapi" / "tickets.openapi.yaml").write_text("openapi: 3.1.0\ninfo: {title: Tickets, version: '1.0.0'}\npaths: {}\n")
            (root / "fixtures" / "overlays" / "tickets.overlay.yaml").write_text("overlay: 1.0.0\ninfo: {title: Overlay, version: '1.0.0'}\nactions: []\n")
            (root / "fixtures" / "workflows" / "tickets.arazzo.yaml").write_text("arazzo: 1.0.0\ninfo: {title: Workflow, version: '1.0.0'}\nworkflows: []\n")
            (root / "fixtures" / "config" / "invalid.cli.json").write_text(json.dumps({
                "cli": "1.0.0",
                "mode": {"default": "discover"},
                "sources": {
                    "badRemote": {
                        "type": "mcp",
                        "transport": {"type": "stdio"},
                        "oauth": {
                            "mode": "clientCredentials",
                            "tokenURL": "https://auth.example.com/oauth/token",
                            "clientId": {"type": "env", "value": "CLIENT_ID"},
                            "clientSecret": {"type": "env", "value": "CLIENT_SECRET"}
                        }
                    }
                }
            }))
            (schema_root / "cli.schema.json").write_text(json.dumps({
                "$schema": "https://json-schema.org/draft/2020-12/schema",
                "type": "object",
                "required": ["cli", "mode"],
                "$defs": {
                    "secretRef": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["env", "file", "osKeychain", "exec"]},
                            "value": {"type": "string", "minLength": 1}
                        },
                        "additionalProperties": False
                    },
                    "mcpOAuthConfig": {
                        "type": "object",
                        "required": ["mode"],
                        "properties": {
                            "mode": {"const": "clientCredentials"},
                            "tokenURL": {"type": "string", "minLength": 1},
                            "clientId": {"$ref": "#/$defs/secretRef"},
                            "clientSecret": {"$ref": "#/$defs/secretRef"}
                        },
                        "additionalProperties": False
                    },
                    "mcpTransport": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["stdio", "sse", "streamable-http"]},
                            "command": {"type": "string", "minLength": 1},
                            "url": {"type": "string", "minLength": 1}
                        },
                        "allOf": [
                            {
                                "if": {"properties": {"type": {"const": "stdio"}}, "required": ["type"]},
                                "then": {"required": ["command"]}
                            },
                            {
                                "if": {"properties": {"type": {"const": "streamable-http"}}, "required": ["type"]},
                                "then": {"required": ["url"]}
                            }
                        ],
                        "additionalProperties": True
                    },
                    "source": {
                        "type": "object",
                        "required": ["type"],
                        "properties": {
                            "type": {"enum": ["mcp", "openapi", "serviceRoot", "apiCatalog"]},
                            "transport": {"$ref": "#/$defs/mcpTransport"},
                            "oauth": {"$ref": "#/$defs/mcpOAuthConfig"}
                        },
                        "allOf": [
                            {
                                "if": {"properties": {"type": {"const": "mcp"}}, "required": ["type", "oauth"]},
                                "then": {"properties": {"transport": {"properties": {"type": {"const": "streamable-http"}}, "required": ["type"]}}}
                            }
                        ],
                        "additionalProperties": True
                    }
                },
                "properties": {
                    "cli": {"type": "string"},
                    "mode": {"type": "object"},
                    "sources": {"type": "object", "additionalProperties": {"$ref": "#/$defs/source"}}
                }
            }))

            with mock.patch.object(run_conformance, "ROOT", root):
                with self.assertRaises(SystemExit):
                    run_conformance.validate_fixture_shapes(schema_root)


if __name__ == "__main__":
    unittest.main()

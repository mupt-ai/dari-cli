from __future__ import annotations

import json
from pathlib import Path
import shutil
import subprocess

import pytest

from dari_cli.manifest import ManifestValidationError, load_manifest, parse_manifest_text


def build_manifest(
    *,
    entrypoint: str = "src/agent.ts:agent",
    sdk: str = "opencode",
    extra_lines: list[str] | None = None,
) -> str:
    lines = [
        "name: support-agent",
        f"sdk: {sdk}",
        f'entrypoint: "{entrypoint}"',
    ]
    if extra_lines:
        lines.extend(extra_lines)
    return "\n".join(lines)


def run_dari_command(*args: str) -> subprocess.CompletedProcess[str]:
    dari = shutil.which("dari")
    assert dari is not None

    return subprocess.run(
        [dari, *args],
        capture_output=True,
        check=False,
        text=True,
    )


def test_load_manifest_parses_documented_sections(tmp_path) -> None:
    manifest_path = tmp_path / "dari.yml"
    manifest_path.write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
                "",
                "runtime:",
                "  language: python",
                "  version: '3.11'",
                "  timeout_seconds: 1800",
                "",
                "retries:",
                "  max_attempts: 3",
                "",
                "secrets:",
                "  - OPENAI_API_KEY",
                "  - INTERNAL_API_TOKEN",
                "",
                "env:",
                "  APP_ENV: production",
            ]
        ),
        encoding="utf-8",
    )

    manifest = load_manifest(tmp_path)

    assert manifest.to_dict() == {
        "name": "support-agent",
        "sdk": "openai-agents",
        "entrypoint": "agent.py:agent",
        "runtime": {
            "language": "python",
            "version": "3.11",
            "timeout_seconds": 1800,
        },
        "retries": {"max_attempts": 3},
        "secrets": ["OPENAI_API_KEY", "INTERNAL_API_TOKEN"],
        "env": {"APP_ENV": "production"},
    }


@pytest.mark.parametrize("raw_text", ["", "  \n", "# comment only\n"])
def test_empty_manifest_documents_report_missing_required_fields(raw_text: str) -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(raw_text)

    message = str(exc_info.value)
    assert "name: field is required" in message
    assert "sdk: field is required" in message
    assert "entrypoint: field is required" in message


@pytest.mark.parametrize("raw_text", ["[]\n", "false\n", "0\n", "null\n", "~\n", '""\n'])
def test_non_mapping_top_level_documents_are_rejected(raw_text: str) -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(raw_text)

    assert "manifest: expected a top-level mapping" in str(exc_info.value)


def test_missing_required_fields_report_field_paths() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text("runtime:\n  language: python\n")

    message = str(exc_info.value)
    assert "name: field is required" in message
    assert "sdk: field is required" in message
    assert "entrypoint: field is required" in message


def test_invalid_secret_shape_is_rejected_as_value_storage() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            build_manifest(
                extra_lines=[
                    "secrets:",
                    "  OPENAI_API_KEY: sk-live-not-allowed",
                ]
            )
        )

    assert (
        "secrets: expected a list of secret names; secret values must not live in the manifest"
        in str(exc_info.value)
    )


def test_invalid_secret_name_is_rejected() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            build_manifest(
                extra_lines=[
                    "secrets:",
                    "  - openai_api_key",
                ]
            )
        )

    assert "secrets[0]: expected a secret name matching ^[A-Z_][A-Z0-9_]*$" in str(
        exc_info.value
    )


def test_secret_names_must_not_overlap_env_keys() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            build_manifest(
                extra_lines=[
                    "secrets:",
                    "  - OPENAI_API_KEY",
                    "env:",
                    "  OPENAI_API_KEY: from-manifest",
                ]
            )
        )

    assert (
        "secrets: secret names must not overlap env keys: OPENAI_API_KEY"
        in str(exc_info.value)
    )


def test_unknown_fields_and_invalid_entrypoint_are_reported() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            build_manifest(
                entrypoint="src/agent.ts",
                extra_lines=[
                    "browser:",
                    "  enabled: true",
                ],
            )
        )

    message = str(exc_info.value)
    assert "browser: unsupported field" in message
    assert "entrypoint: expected a '<module-or-path>:<export>' reference" in message


@pytest.mark.parametrize("entrypoint", [":agent", "src/agent.ts:", "a:b:c", " :agent", "src/agent.ts: "])
def test_invalid_entrypoint_shapes_are_rejected(entrypoint: str) -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(build_manifest(entrypoint=entrypoint))

    assert "entrypoint: expected a '<module-or-path>:<export>' reference" in str(exc_info.value)


def test_bool_values_are_rejected_for_integer_fields() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            build_manifest(
                entrypoint="agent.py:agent",
                sdk="openai-agents",
                extra_lines=[
                    "runtime:",
                    "  timeout_seconds: true",
                    "retries:",
                    "  max_attempts: false",
                ],
            )
        )

    message = str(exc_info.value)
    assert "runtime.timeout_seconds: expected a positive integer" in message
    assert "retries.max_attempts: expected a positive integer" in message


def test_mixed_key_types_report_validation_errors_instead_of_crashing() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            "\n".join(
                [
                    "name: support-agent",
                    "sdk: opencode",
                    "entrypoint: src/agent.ts:agent",
                    "runtime:",
                    "  language: typescript",
                    "  1: invalid",
                    "1: top-level-invalid",
                ]
            )
        )

    message = str(exc_info.value)
    assert "1: unsupported field" in message
    assert "runtime.1: unsupported field" in message


def test_missing_sdk_reports_only_the_required_issue() -> None:
    with pytest.raises(ManifestValidationError) as exc_info:
        parse_manifest_text(
            "\n".join(
                [
                    "name: support-agent",
                    "entrypoint: agent.py:agent",
                ]
            )
        )

    message = str(exc_info.value)
    assert "sdk: field is required" in message
    assert "sdk: expected one of" not in message


def test_pi_sdk_is_accepted_for_typescript_manifests() -> None:
    manifest = parse_manifest_text(build_manifest(sdk="pi"))

    assert manifest.to_dict() == {
        "name": "support-agent",
        "sdk": "pi",
        "entrypoint": "src/agent.ts:agent",
    }


@pytest.mark.parametrize(
    ("example_dir", "expected_sdk", "expected_entrypoint"),
    [
        ("hello-opencode", "opencode", "src/agent.ts:agent"),
        ("hello-pi", "pi", "src/agent.js:agent"),
        (
            "hello-claude-agent-sdk-js",
            "claude-agent-sdk",
            "src/agent.js:agent",
        ),
        (
            "hello-claude-agent-sdk-python",
            "claude-agent-sdk",
            "agent.py:agent",
        ),
        ("hello-openai-agents-js", "openai-agents", "src/agent.js:agent"),
        (
            "hello-openai-agents-python",
            "openai-agents",
            "agent.py:agent",
        ),
    ],
)
def test_checked_in_examples_have_valid_manifests(
    example_dir: str,
    expected_sdk: str,
    expected_entrypoint: str,
) -> None:
    repo_root = Path(__file__).resolve().parents[1]

    manifest = load_manifest(repo_root / "examples" / example_dir)

    assert manifest.name == example_dir
    assert manifest.sdk == expected_sdk
    assert manifest.entrypoint == expected_entrypoint


def test_dari_console_script_validates_manifest(tmp_path) -> None:
    manifest_path = tmp_path / "dari.yml"
    manifest_path.write_text(build_manifest(), encoding="utf-8")

    result = run_dari_command("manifest", "validate", str(tmp_path), "--json")

    assert result.returncode == 0
    assert result.stderr == ""
    assert json.loads(result.stdout) == {
        "name": "support-agent",
        "sdk": "opencode",
        "entrypoint": "src/agent.ts:agent",
    }


def test_dari_console_script_reports_missing_manifest(tmp_path) -> None:
    result = run_dari_command("manifest", "validate", str(tmp_path))

    assert result.returncode == 1
    assert result.stdout == ""
    assert result.stderr == f"Manifest file not found: {tmp_path / 'dari.yml'}\n"


def test_dari_console_script_rejects_file_repo_root(tmp_path) -> None:
    repo_file = tmp_path / "repo.txt"
    repo_file.write_text("not-a-directory", encoding="utf-8")

    result = run_dari_command("manifest", "validate", str(repo_file))

    assert result.returncode == 1
    assert result.stdout == ""
    assert result.stderr == f"Repository root must be a directory: {repo_file}\n"


def test_dari_console_script_reports_manifest_validation_errors(tmp_path) -> None:
    manifest_path = tmp_path / "dari.yml"
    manifest_path.write_text(build_manifest(entrypoint=":agent"), encoding="utf-8")

    result = run_dari_command("manifest", "validate", str(tmp_path))

    assert result.returncode == 1
    assert result.stdout == ""
    assert "Manifest validation failed for" in result.stderr
    assert "entrypoint: expected a '<module-or-path>:<export>' reference" in result.stderr

from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from dari_cli.manifest import (
    ManifestValidationError,
    load_manifest,
    parse_manifest_text,
)


def write_valid_bundle(repo_root: Path) -> None:
    (repo_root / "prompts").mkdir(parents=True, exist_ok=True)
    (repo_root / "prompts" / "system.md").write_text(
        "You are a managed test bundle.\n",
        encoding="utf-8",
    )
    (repo_root / "Dockerfile").write_text(
        "\n".join(
            [
                "FROM node:20-bookworm",
                "WORKDIR /bundle",
                "COPY . /bundle",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search").mkdir(parents=True, exist_ok=True)
    (repo_root / "tools" / "repo_search" / "tool.yml").write_text(
        "\n".join(
            [
                "name: repo_search",
                "description: Search the repository for matching content.",
                "input_schema: input.schema.json",
                "output_schema: output.schema.json",
                "runtime: typescript",
                "handler: handler.ts:main",
                "retries: 3",
                "timeout_seconds: 20",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search" / "handler.ts").write_text(
        "export async function main() { return { ok: true }; }\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search" / "input.schema.json").write_text(
        '{"type":"object","properties":{"query":{"type":"string"}}}',
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search" / "output.schema.json").write_text(
        '{"type":"object","properties":{"matches":{"type":"array"}}}',
        encoding="utf-8",
    )
    (repo_root / "assets").mkdir(exist_ok=True)
    (repo_root / "assets" / "examples.json").write_text(
        "[]\n",
        encoding="utf-8",
    )
    (repo_root / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: opencode",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: repo_search_fast",
                "    path: tools/repo_search",
                "    kind: ephemeral",
                "  - name: bash",
                "env:",
                "  APP_ENV: production",
                "secrets:",
                "  - OPENAI_API_KEY",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def write_valid_skill(repo_root: Path) -> None:
    (repo_root / "skills" / "review").mkdir(parents=True, exist_ok=True)
    (repo_root / "skills" / "review" / "SKILL.md").write_text(
        "\n".join(
            [
                "---",
                "name: review",
                "description: Review code changes.",
                "disable-model-invocation: true",
                "---",
                "",
                "Review the current code changes.",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def run_dari_command(*args: str) -> subprocess.CompletedProcess[str]:
    dari = shutil.which("dari")
    assert dari is not None
    return subprocess.run(
        [dari, *args],
        capture_output=True,
        check=False,
        text=True,
    )


def test_load_manifest_discovers_custom_tools_and_root_overrides(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "tools" / "jira_lookup").mkdir()
    (tmp_path / "tools" / "jira_lookup" / "tool.yml").write_text(
        "\n".join(
            [
                "name: jira_lookup",
                "description: Look up issues by key.",
                "input_schema: input.schema.json",
                "runtime: python",
                "handler: handler.py:main",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "tools" / "jira_lookup" / "handler.py").write_text(
        "def main():\n    return {'ok': True}\n",
        encoding="utf-8",
    )
    (tmp_path / "tools" / "jira_lookup" / "input.schema.json").write_text(
        '{"type":"object","properties":{"key":{"type":"string"}}}',
        encoding="utf-8",
    )

    manifest = load_manifest(tmp_path)

    assert manifest.to_dict() == {
        "built_in_tools": [
            {"execution_mode": "main", "name": "bash"},
        ],
        "custom_tools": [
            {
                "description": "Look up issues by key.",
                "execution_mode": "client",
                "handler": "tools/jira_lookup/handler.py:main",
                "input_schema": "tools/jira_lookup/input.schema.json",
                "name": "jira_lookup",
                "runtime": "python",
                "source_name": "jira_lookup",
                "source_path": "tools/jira_lookup",
            },
            {
                "description": "Search the repository for matching content.",
                "execution_mode": "ephemeral",
                "handler": "tools/repo_search/handler.ts:main",
                "input_schema": "tools/repo_search/input.schema.json",
                "name": "repo_search_fast",
                "output_schema": "tools/repo_search/output.schema.json",
                "retries": 3,
                "runtime": "typescript",
                "source_name": "repo_search",
                "source_path": "tools/repo_search",
                "timeout_seconds": 20,
            },
        ],
        "env": {"APP_ENV": "production"},
        "harness": "opencode",
        "instructions": {"system": "prompts/system.md"},
        "name": "support-agent",
        "runtime": {"dockerfile": "Dockerfile"},
        "secrets": ["OPENAI_API_KEY"],
    }


def test_parse_manifest_text_uses_manifest_path_parent_as_repo_root(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    manifest_path = tmp_path / "dari.yml"

    manifest = parse_manifest_text(
        manifest_path.read_text(encoding="utf-8"),
        manifest_path,
    )

    assert manifest.harness == "opencode"
    assert manifest.instructions.system == "prompts/system.md"


def test_missing_required_fields_are_reported(tmp_path: Path) -> None:
    (tmp_path / "dari.yml").write_text(
        "env:\n  APP_ENV: production\n", encoding="utf-8"
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    message = str(exc_info.value)
    assert "name: field is required" in message
    assert "harness: field is required" in message
    assert "instructions: field is required" in message
    assert "runtime: field is required" in message


def test_root_entrypoint_is_rejected_with_migration_message(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: opencode",
                "entrypoint: src/agent.ts:agent",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert "entrypoint: agent-level entrypoints are no longer supported" in str(
        exc_info.value
    )


def test_unknown_fields_and_invalid_tool_runtime_are_rejected(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        (tmp_path / "dari.yml").read_text(encoding="utf-8")
        + "browser:\n  enabled: true\n"
    )
    (tmp_path / "tools" / "repo_search" / "tool.yml").write_text(
        "\n".join(
            [
                "name: repo_search",
                "description: Search the repository for matching content.",
                "input_schema: input.schema.json",
                "runtime: ruby",
                "handler: handler.ts:main",
                "extra_field: true",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    message = str(exc_info.value)
    assert "browser: unsupported field" in message
    assert "tools/repo_search.tool.extra_field: unsupported field" in message
    assert (
        "tools/repo_search.tool.runtime: expected one of 'typescript', 'python'"
        in message
    )


def test_missing_dockerfile_tool_yml_and_bad_handler_are_rejected(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "Dockerfile").unlink()
    (tmp_path / "tools" / "repo_search" / "tool.yml").write_text(
        "\n".join(
            [
                "name: repo_search",
                "description: Search the repository for matching content.",
                "input_schema: input.schema.json",
                "runtime: typescript",
                "handler: handler.ts",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "tools" / "extra").mkdir()

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    message = str(exc_info.value)
    assert "runtime.dockerfile: file does not exist at repo root" in message
    assert "tools/extra.tool.yml: tool.yml file is required" in message
    assert (
        "tools/repo_search.tool.handler: expected a '<module-or-path>:<export>' reference"
        in message
    )


def test_duplicate_tool_names_are_rejected(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: opencode",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: bash",
                "  - name: bash",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert "duplicate tool name 'bash'" in str(exc_info.value)


def test_built_in_tool_entry_without_kind_defaults_to_main(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: opencode",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: read",
                "  - name: grep",
                "    kind: ephemeral",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    manifest = load_manifest(tmp_path)

    assert [tool.to_dict() for tool in manifest.built_in_tools] == [
        {"execution_mode": "main", "name": "read"},
        {"execution_mode": "ephemeral", "name": "grep"},
    ]


def test_unknown_built_in_tool_name_is_rejected(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: opencode",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: sandbox.exec",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert (
        "tools[0].name: entries without 'path' must reference a built-in tool"
        in str(exc_info.value)
    )


def test_manifest_validate_json_outputs_normalized_payload(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)

    result = run_dari_command("manifest", "validate", str(tmp_path), "--json")

    assert result.returncode == 0
    payload = json.loads(result.stdout)
    assert payload["name"] == "support-agent"
    assert payload["custom_tools"][0]["execution_mode"] == "ephemeral"


def test_load_manifest_normalizes_root_skills(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    write_valid_skill(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: repo_search_fast",
                "    path: tools/repo_search",
                "    kind: ephemeral",
                "skills:",
                "  - name: review",
                "    path: skills/review",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    manifest = load_manifest(tmp_path)

    assert manifest.to_dict()["skills"] == [
        {
            "name": "review",
            "source_path": "skills/review",
            "skill_file": "skills/review/SKILL.md",
            "description": "Review code changes.",
            "disable_model_invocation": True,
        }
    ]


def test_root_skills_duplicate_name_and_path_are_rejected(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    write_valid_skill(tmp_path)
    (tmp_path / "skills" / "other").mkdir(parents=True)
    (tmp_path / "skills" / "other" / "SKILL.md").write_text(
        "\n".join(
            [
                "---",
                "name: other",
                "description: Another skill.",
                "---",
                "",
                "Another skill body.",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "skills:",
                "  - name: review",
                "    path: skills/review",
                "  - name: review",
                "    path: skills/other",
                "  - name: third",
                "    path: skills/review",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    message = str(exc_info.value)
    assert "skills[1].name: duplicate declared skill name 'review'" in message
    assert "skills[2].path: duplicate declared skill path 'skills/review'" in message


def test_root_skills_must_live_under_skills_directory(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "skills:",
                "  - name: review",
                "    path: tools/repo_search",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert "skills[0].path: expected a path under skills/" in str(exc_info.value)


def test_root_skill_declared_name_must_match_effective_name(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "skills" / "third").mkdir(parents=True)
    (tmp_path / "skills" / "third" / "SKILL.md").write_text(
        "\n".join(
            [
                "---",
                "name: third",
                "description: Third skill.",
                "---",
                "",
                "Third skill body.",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "skills:",
                "  - name: mismatch",
                "    path: skills/third",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert (
        "skills[0].name: declared skill name 'mismatch' did not match effective skill name 'third'"
        in str(exc_info.value)
    )


def test_root_skill_parent_directory_name_must_match_skill_name(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "skills" / "mismatch-dir").mkdir(parents=True)
    (tmp_path / "skills" / "mismatch-dir" / "SKILL.md").write_text(
        "\n".join(
            [
                "---",
                "name: other",
                "description: Another skill.",
                "---",
                "",
                "Another skill body.",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "skills:",
                "  - name: mismatch-dir",
                "    path: skills/mismatch-dir",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    with pytest.raises(ManifestValidationError) as exc_info:
        load_manifest(tmp_path)

    assert (
        "skills/mismatch-dir/SKILL.md.name: skill name must match parent directory name 'mismatch-dir'"
        in str(exc_info.value)
    )


def test_manifest_validate_json_includes_normalized_skills(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    write_valid_skill(tmp_path)
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "skills:",
                "  - name: review",
                "    path: skills/review",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    result = run_dari_command("manifest", "validate", str(tmp_path), "--json")

    assert result.returncode == 0
    payload = json.loads(result.stdout)
    assert payload["skills"] == [
        {
            "name": "review",
            "source_path": "skills/review",
            "skill_file": "skills/review/SKILL.md",
            "description": "Review code changes.",
            "disable_model_invocation": True,
        }
    ]

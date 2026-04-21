from __future__ import annotations

import io
import json
import tarfile
from pathlib import Path

import pytest

from dari_cli.__main__ import main
from dari_cli.deploy import build_source_bundle
from dari_cli.init import InitError, init_project


EXPECTED_RELATIVE_FILES = {
    "dari.yml",
    "Dockerfile",
    "prompts/system.md",
    "tools/repo_search/tool.yml",
    "tools/repo_search/input.schema.json",
    "tools/repo_search/handler.ts",
    "skills/review/SKILL.md",
    "README.md",
    ".gitignore",
}


def test_init_project_writes_hello_pi_layout_with_skill(tmp_path: Path) -> None:
    project_dir = tmp_path / "my-agent"

    result = init_project(project_dir)

    assert result.project_root == project_dir.resolve()
    assert result.project_name == "my-agent"
    assert result.skill_name == "review"

    written_relative = {
        path.relative_to(project_dir.resolve()).as_posix()
        for path in result.written_files
    }
    assert written_relative == EXPECTED_RELATIVE_FILES

    dari_yml = (project_dir / "dari.yml").read_text(encoding="utf-8")
    assert "name: my-agent\n" in dari_yml
    assert "harness: pi\n" in dari_yml
    assert "- name: repo_search" in dari_yml
    assert "skills:\n  - name: review\n    path: skills/review\n" in dari_yml

    skill_body = (project_dir / "skills" / "review" / "SKILL.md").read_text(
        encoding="utf-8"
    )
    assert skill_body.startswith("---\nname: review\n")
    assert "description:" in skill_body


def test_init_project_uses_directory_name_and_custom_skill(tmp_path: Path) -> None:
    project_dir = tmp_path / "Support Agent"
    project_dir.mkdir()

    result = init_project(project_dir, skill="summarize")

    assert result.project_name == "support-agent"
    assert result.skill_name == "summarize"

    dari_yml = (project_dir / "dari.yml").read_text(encoding="utf-8")
    assert "name: support-agent\n" in dari_yml
    assert "- name: summarize\n    path: skills/summarize\n" in dari_yml
    assert (project_dir / "skills" / "summarize" / "SKILL.md").exists()


def test_init_project_accepts_explicit_name(tmp_path: Path) -> None:
    result = init_project(tmp_path, name="custom-name")

    assert result.project_name == "custom-name"
    assert "name: custom-name\n" in (tmp_path / "dari.yml").read_text(encoding="utf-8")


def test_init_project_rejects_invalid_name(tmp_path: Path) -> None:
    with pytest.raises(InitError, match="Project name"):
        init_project(tmp_path, name="Invalid Name!")


def test_init_project_rejects_invalid_skill(tmp_path: Path) -> None:
    with pytest.raises(InitError, match="Skill name"):
        init_project(tmp_path, skill="Bad Skill")


def test_init_project_refuses_to_overwrite_without_force(tmp_path: Path) -> None:
    (tmp_path / "dari.yml").write_text("existing\n", encoding="utf-8")

    with pytest.raises(InitError, match="Refusing to overwrite"):
        init_project(tmp_path, name="agent")

    assert (tmp_path / "dari.yml").read_text(encoding="utf-8") == "existing\n"


def test_init_project_force_overwrites_existing_files(tmp_path: Path) -> None:
    (tmp_path / "dari.yml").write_text("existing\n", encoding="utf-8")

    init_project(tmp_path, name="agent", force=True)

    assert "name: agent\n" in (tmp_path / "dari.yml").read_text(encoding="utf-8")


def test_init_project_output_deploys_cleanly(tmp_path: Path) -> None:
    init_project(tmp_path, name="agent")

    bundle = build_source_bundle(tmp_path)

    with tarfile.open(fileobj=io.BytesIO(bundle.content), mode="r:gz") as archive:
        names = set(archive.getnames())

    assert EXPECTED_RELATIVE_FILES.issubset(names)


def test_init_cli_prints_summary_json(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    exit_code = main(["init", str(tmp_path), "--name", "agent"])

    assert exit_code == 0

    captured = capsys.readouterr()
    payload = json.loads(captured.out)
    assert payload["project_name"] == "agent"
    assert payload["skill_name"] == "review"
    assert payload["project_root"] == str(tmp_path.resolve())
    assert set(payload["written_files"]) == EXPECTED_RELATIVE_FILES


def test_init_cli_reports_conflict_error(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    (tmp_path / "dari.yml").write_text("existing\n", encoding="utf-8")

    exit_code = main(["init", str(tmp_path), "--name", "agent"])

    assert exit_code == 1
    captured = capsys.readouterr()
    assert "Refusing to overwrite" in captured.err

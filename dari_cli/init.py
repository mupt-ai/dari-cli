"""Scaffold a new Dari agent project in a target directory."""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence

DEFAULT_SKILL_NAME = "review"
DEFAULT_PROJECT_NAME = "my-agent"
PROJECT_NAME_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]*$")
SKILL_NAME_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]*$")


class InitError(RuntimeError):
    """Raised when the init command cannot scaffold the project."""


@dataclass(frozen=True)
class InitResult:
    """Files written by a successful init run."""

    project_root: Path
    project_name: str
    skill_name: str
    written_files: tuple[Path, ...]


def init_project(
    target_dir: str | Path,
    *,
    name: str | None = None,
    skill: str = DEFAULT_SKILL_NAME,
    force: bool = False,
) -> InitResult:
    """Create a base Dari project under ``target_dir``.

    Mirrors the ``hello-pi`` example and adds a skill alongside it.
    """
    project_root = Path(target_dir).resolve()
    project_name = _resolve_project_name(name, project_root)
    skill_name = _validate_skill_name(skill)

    project_root.mkdir(parents=True, exist_ok=True)

    files = _render_project_files(project_name, skill_name)
    if not force:
        _ensure_no_conflicts(project_root, files)

    written: list[Path] = []
    for relative_path, contents in files:
        destination = project_root / relative_path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(contents, encoding="utf-8")
        written.append(destination)

    return InitResult(
        project_root=project_root,
        project_name=project_name,
        skill_name=skill_name,
        written_files=tuple(written),
    )


def _resolve_project_name(name: str | None, project_root: Path) -> str:
    if name is not None:
        return _validate_project_name(name)
    candidate = _slugify(project_root.name) or DEFAULT_PROJECT_NAME
    return _validate_project_name(candidate)


def _validate_project_name(name: str) -> str:
    normalized = name.strip()
    if not PROJECT_NAME_PATTERN.fullmatch(normalized):
        raise InitError(
            "Project name must match [a-z0-9][a-z0-9-]* "
            f"(got {name!r}). Pass --name to override."
        )
    return normalized


def _validate_skill_name(name: str) -> str:
    normalized = name.strip()
    if not SKILL_NAME_PATTERN.fullmatch(normalized):
        raise InitError(
            "Skill name must match [a-z0-9][a-z0-9-]* "
            f"(got {name!r})."
        )
    return normalized


def _slugify(value: str) -> str:
    lowered = value.strip().lower()
    replaced = re.sub(r"[^a-z0-9]+", "-", lowered).strip("-")
    return replaced


def _ensure_no_conflicts(
    project_root: Path,
    files: Sequence[tuple[str, str]],
) -> None:
    existing = [
        relative_path for relative_path, _ in files
        if (project_root / relative_path).exists()
    ]
    if existing:
        conflict_list = "\n  ".join(existing)
        raise InitError(
            "Refusing to overwrite existing files (pass --force to overwrite):\n  "
            + conflict_list
        )


def _render_project_files(
    project_name: str,
    skill_name: str,
) -> list[tuple[str, str]]:
    return [
        ("dari.yml", _render_dari_yml(project_name, skill_name)),
        ("Dockerfile", _render_dockerfile()),
        ("prompts/system.md", _render_system_prompt(skill_name)),
        ("tools/repo_search/tool.yml", _render_tool_yml()),
        ("tools/repo_search/input.schema.json", _render_tool_input_schema()),
        ("tools/repo_search/handler.ts", _render_tool_handler()),
        (f"skills/{skill_name}/SKILL.md", _render_skill_md(skill_name)),
        ("README.md", _render_readme(project_name, skill_name)),
        (".gitignore", _render_gitignore()),
    ]


def _render_dari_yml(project_name: str, skill_name: str) -> str:
    return (
        f"name: {project_name}\n"
        "harness: pi\n"
        "\n"
        "instructions:\n"
        "  system: prompts/system.md\n"
        "\n"
        "runtime:\n"
        "  dockerfile: Dockerfile\n"
        "\n"
        "sandbox:\n"
        "  provider: e2b\n"
        "  provider_api_key_secret: E2B_API_KEY\n"
        "\n"
        "llm:\n"
        "  model: anthropic/claude-sonnet-4.6\n"
        "  base_url: https://openrouter.ai/api/v1\n"
        "  api_key_secret: OPENROUTER_API_KEY\n"
        "\n"
        "tools:\n"
        "  - name: repo_search\n"
        "    path: tools/repo_search\n"
        "    kind: main\n"
        "\n"
        "skills:\n"
        f"  - name: {skill_name}\n"
        f"    path: skills/{skill_name}\n"
        "\n"
        "env:\n"
        "  APP_ENV: example\n"
    )


def _render_dockerfile() -> str:
    return (
        "FROM node:20-bookworm\n"
        "\n"
        "WORKDIR /bundle\n"
        "COPY . /bundle\n"
    )


def _render_system_prompt(skill_name: str) -> str:
    return (
        "You are a managed agent bundle.\n"
        "Use `repo_search` before answering questions about checked-in files.\n"
        f"Load the `{skill_name}` skill when the user asks you to apply it.\n"
    )


def _render_tool_yml() -> str:
    return (
        "name: repo_search\n"
        "description: Search the checked-out repository for matching content.\n"
        "input_schema: input.schema.json\n"
        "runtime: typescript\n"
        "handler: handler.ts:main\n"
        "retries: 2\n"
        "timeout_seconds: 20\n"
    )


def _render_tool_input_schema() -> str:
    return (
        "{\n"
        '  "type": "object",\n'
        '  "properties": {\n'
        '    "query": {\n'
        '      "type": "string"\n'
        "    }\n"
        "  },\n"
        '  "required": ["query"],\n'
        '  "additionalProperties": false\n'
        "}\n"
    )


def _render_tool_handler() -> str:
    return (
        "export async function main(input: { query: string }) {\n"
        "  return {\n"
        "    matches: [`matched: ${input.query}`],\n"
        "  };\n"
        "}\n"
    )


def _render_skill_md(skill_name: str) -> str:
    title = skill_name.replace("-", " ").title()
    return (
        "---\n"
        f"name: {skill_name}\n"
        f"description: {title} playbook for this agent.\n"
        "---\n"
        "\n"
        f"# {title}\n"
        "\n"
        "Steps to follow when the agent is asked to apply this skill:\n"
        "\n"
        "1. Gather the relevant files with `repo_search`.\n"
        "2. Summarize what you found before taking action.\n"
        "3. Reply with concrete, specific recommendations.\n"
    )


def _render_readme(project_name: str, skill_name: str) -> str:
    return (
        f"# {project_name}\n"
        "\n"
        "Scaffolded with `dari init`. Validate and deploy from this directory:\n"
        "\n"
        "```bash\n"
        "dari manifest validate\n"
        "dari deploy --dry-run\n"
        "dari deploy\n"
        "```\n"
        "\n"
        "Before a real deploy, upload any referenced credentials:\n"
        "\n"
        "```bash\n"
        "dari credentials add OPENROUTER_API_KEY\n"
        "dari credentials add E2B_API_KEY\n"
        "```\n"
        "\n"
        "## Layout\n"
        "\n"
        "- `dari.yml` — manifest\n"
        "- `prompts/system.md` — system prompt\n"
        "- `tools/repo_search/` — example tool\n"
        f"- `skills/{skill_name}/SKILL.md` — example skill\n"
    )


def _render_gitignore() -> str:
    return ".dari/\n.env\n"

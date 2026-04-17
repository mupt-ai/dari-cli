"""Managed bundle parsing and validation for repo-root ``dari.yml`` files."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path, PurePosixPath
import re
from typing import Any, Literal, Mapping

import yaml

SUPPORTED_HARNESSES = (
    "openai-agents",
    "claude-agent-sdk",
    "opencode",
    "pi",
)
SUPPORTED_TOOL_RUNTIMES = ("typescript", "python")
TOP_LEVEL_FIELDS = {
    "name",
    "harness",
    "instructions",
    "runtime",
    "sandbox",
    "llm",
    "tools",
    "secrets",
    "env",
}
INSTRUCTIONS_FIELDS = {"system"}
RUNTIME_FIELDS = {"dockerfile"}
SANDBOX_FIELDS = {"provider", "provider_api_key_secret"}
LLM_FIELDS = {"model", "base_url", "api_key_secret"}
ROOT_TOOL_FIELDS = {"name", "path", "kind"}
TOOL_FIELDS = {
    "name",
    "description",
    "input_schema",
    "output_schema",
    "runtime",
    "handler",
    "retries",
    "timeout_seconds",
}
ENVIRONMENT_VARIABLE_NAME_PATTERN = re.compile(r"^[A-Z_][A-Z0-9_]*$")
EXECUTION_MODES = ("client", "main", "ephemeral")
ROOT_TOOL_KINDS = ("main", "ephemeral")
SUPPORTED_SANDBOX_PROVIDERS = ("e2b",)
OPENAI_API_KEY_ENV_NAME = "OPENAI_API_KEY"


@dataclass(frozen=True)
class ManifestIssue:
    """A single validation issue for the manifest."""

    path: str
    message: str


@dataclass(frozen=True)
class BundleInstructions:
    """Normalized prompt/instruction references for one bundle."""

    system: str

    def to_dict(self) -> dict[str, str]:
        return {"system": self.system}


@dataclass(frozen=True)
class BundleRuntime:
    """Normalized runtime build metadata for one bundle."""

    dockerfile: str

    def to_dict(self) -> dict[str, str]:
        return {"dockerfile": self.dockerfile}


@dataclass(frozen=True)
class BundleSandbox:
    """Normalized sandbox provider settings for one bundle."""

    provider: Literal["e2b"]
    provider_api_key_secret: str

    def to_dict(self) -> dict[str, str]:
        return {
            "provider": self.provider,
            "provider_api_key_secret": self.provider_api_key_secret,
        }


@dataclass(frozen=True)
class BundleLlm:
    """Normalized LLM provider settings for one bundle."""

    model: str
    base_url: str
    api_key_secret: str

    def to_dict(self) -> dict[str, str]:
        return {
            "model": self.model,
            "base_url": self.base_url,
            "api_key_secret": self.api_key_secret,
        }


@dataclass(frozen=True)
class BuiltInTool:
    """One built-in/default tool exposed by the root manifest."""

    name: str
    execution_mode: Literal["main", "ephemeral"]

    def to_dict(self) -> dict[str, str]:
        return {
            "name": self.name,
            "execution_mode": self.execution_mode,
        }


@dataclass(frozen=True)
class CustomTool:
    """One discovered custom tool in the normalized bundle payload."""

    name: str
    source_name: str
    source_path: str
    description: str
    input_schema: str
    runtime: Literal["typescript", "python"]
    handler: str
    execution_mode: Literal["client", "main", "ephemeral"] = "client"
    output_schema: str | None = None
    retries: int | None = None
    timeout_seconds: int | None = None

    def to_dict(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "name": self.name,
            "source_name": self.source_name,
            "source_path": self.source_path,
            "description": self.description,
            "input_schema": self.input_schema,
            "runtime": self.runtime,
            "handler": self.handler,
            "execution_mode": self.execution_mode,
        }
        if self.output_schema is not None:
            payload["output_schema"] = self.output_schema
        if self.retries is not None:
            payload["retries"] = self.retries
        if self.timeout_seconds is not None:
            payload["timeout_seconds"] = self.timeout_seconds
        return payload


@dataclass(frozen=True)
class AgentManifest:
    """Normalized managed bundle payload emitted by the CLI."""

    name: str
    harness: Literal["openai-agents", "claude-agent-sdk", "opencode", "pi"]
    instructions: BundleInstructions
    runtime: BundleRuntime
    sandbox: BundleSandbox | None = None
    llm: BundleLlm | None = None
    built_in_tools: tuple[BuiltInTool, ...] = ()
    custom_tools: tuple[CustomTool, ...] = ()
    secrets: tuple[str, ...] = ()
    env: Mapping[str, str] | None = None

    def to_dict(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "name": self.name,
            "harness": self.harness,
            "instructions": self.instructions.to_dict(),
            "runtime": self.runtime.to_dict(),
            "built_in_tools": [tool.to_dict() for tool in self.built_in_tools],
            "custom_tools": [tool.to_dict() for tool in self.custom_tools],
        }
        if self.sandbox is not None:
            payload["sandbox"] = self.sandbox.to_dict()
        if self.llm is not None:
            payload["llm"] = self.llm.to_dict()
        if self.secrets:
            payload["secrets"] = list(self.secrets)
        if self.env:
            payload["env"] = dict(self.env)
        return payload


@dataclass(frozen=True)
class RootToolOverride:
    """One root-manifest tool entry before normalization."""

    name: str
    kind: Literal["main", "ephemeral"]
    path: str | None = None


@dataclass(frozen=True)
class DiscoveredTool:
    """One validated tool discovered from ``tools/<name>/tool.yml``."""

    source_name: str
    source_path: str
    description: str
    input_schema: str
    runtime: Literal["typescript", "python"]
    handler: str
    output_schema: str | None = None
    retries: int | None = None
    timeout_seconds: int | None = None


class ManifestValidationError(ValueError):
    """Raised when ``dari.yml`` cannot be parsed into a valid manifest."""

    def __init__(self, manifest_path: Path, issues: list[ManifestIssue]) -> None:
        self.manifest_path = manifest_path
        self.issues = tuple(issues)
        formatted = "\n".join(
            f"- {issue.path}: {issue.message}" for issue in self.issues
        )
        super().__init__(
            f"Manifest validation failed for {manifest_path}:\n{formatted}"
        )


def load_manifest(repo_root: str | Path) -> AgentManifest:
    """Load and validate the repo-root managed bundle."""
    resolved_root = Path(repo_root).resolve()
    manifest_path = resolved_root / "dari.yml"
    raw_text = manifest_path.read_text(encoding="utf-8")
    return parse_manifest_text(raw_text, manifest_path)


def parse_manifest_text(
    raw_text: str,
    manifest_path: str | Path = "dari.yml",
) -> AgentManifest:
    """Parse and validate manifest text against the managed bundle contract."""
    resolved_path = Path(manifest_path).resolve()
    repo_root = resolved_path.parent
    data = _load_yaml_mapping(raw_text, resolved_path)
    issues: list[ManifestIssue] = []

    entrypoint_present = "entrypoint" in data
    _report_unknown_keys(
        data,
        TOP_LEVEL_FIELDS | {"entrypoint"},
        issues,
    )
    if entrypoint_present:
        issues.append(
            ManifestIssue(
                "entrypoint",
                "agent-level entrypoints are no longer supported; remove the root entrypoint and define per-tool handlers in tools/<name>/tool.yml",
            )
        )

    name = _require_non_empty_string(data, "name", issues)
    harness = _require_harness(data, issues)
    instructions = _parse_instructions(data.get("instructions"), repo_root, issues)
    runtime = _parse_runtime(data.get("runtime"), repo_root, issues)
    sandbox = _parse_sandbox(data.get("sandbox"), issues)
    llm = _parse_llm(data.get("llm"), issues)
    env = _parse_env(data.get("env"), issues)
    secrets = _parse_secrets(data.get("secrets"), issues)
    _report_secret_env_overlap(secrets, env, issues)
    _validate_llm_references(llm, secrets, env, issues)
    _validate_sandbox_references(sandbox, secrets, issues)
    root_tools = _parse_root_tools(data.get("tools"), issues)
    discovered_tools = _discover_custom_tools(repo_root, issues)
    built_in_tools, custom_tools = _build_effective_tools(
        root_tools=root_tools,
        discovered_tools=discovered_tools,
        issues=issues,
    )

    if issues:
        raise ManifestValidationError(resolved_path, issues)

    assert instructions is not None
    assert runtime is not None
    return AgentManifest(
        name=name,
        harness=harness,
        instructions=instructions,
        runtime=runtime,
        sandbox=sandbox,
        llm=llm,
        built_in_tools=tuple(built_in_tools),
        custom_tools=tuple(custom_tools),
        secrets=tuple(secrets),
        env=env,
    )


def _load_yaml_mapping(raw_text: str, manifest_path: Path) -> dict[str, Any]:
    try:
        loaded = yaml.safe_load(raw_text)
    except yaml.YAMLError as exc:
        raise ManifestValidationError(
            manifest_path,
            [ManifestIssue("manifest", f"invalid YAML: {exc}")],
        ) from exc

    if loaded is None and yaml.compose(raw_text) is None:
        loaded = {}

    if not isinstance(loaded, dict):
        raise ManifestValidationError(
            manifest_path,
            [ManifestIssue("manifest", "expected a top-level mapping")],
        )
    return dict(loaded)


def _report_unknown_keys(
    mapping: Mapping[str, Any],
    allowed: set[str],
    issues: list[ManifestIssue],
    *,
    prefix: str = "",
) -> None:
    for key in sorted(mapping, key=str):
        if key not in allowed:
            path = f"{prefix}.{key}" if prefix else str(key)
            issues.append(ManifestIssue(path, "unsupported field"))


def _require_non_empty_string(
    data: Mapping[str, Any],
    field_name: str,
    issues: list[ManifestIssue],
) -> str:
    value = data.get(field_name)
    if value is None:
        issues.append(ManifestIssue(field_name, "field is required"))
        return ""
    return _coerce_non_empty_string(value, field_name, issues)


def _coerce_non_empty_string(
    value: object,
    field_name: str,
    issues: list[ManifestIssue],
) -> str:
    if not isinstance(value, str) or not value.strip():
        issues.append(ManifestIssue(field_name, "expected a non-empty string"))
        return ""
    return value.strip()


def _require_harness(
    data: Mapping[str, Any],
    issues: list[ManifestIssue],
) -> Literal["openai-agents", "claude-agent-sdk", "opencode", "pi"]:
    raw_harness = _require_non_empty_string(data, "harness", issues)
    if not raw_harness:
        return "openai-agents"
    if raw_harness not in SUPPORTED_HARNESSES:
        issues.append(
            ManifestIssue(
                "harness",
                "expected one of "
                + ", ".join(repr(value) for value in SUPPORTED_HARNESSES),
            )
        )
        return "openai-agents"
    return raw_harness  # type: ignore[return-value]


def _parse_instructions(
    value: object,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> BundleInstructions | None:
    if value is None:
        issues.append(ManifestIssue("instructions", "field is required"))
        return None
    if not isinstance(value, Mapping):
        issues.append(ManifestIssue("instructions", "expected a mapping"))
        return None

    _report_unknown_keys(value, INSTRUCTIONS_FIELDS, issues, prefix="instructions")
    system_ref = _coerce_non_empty_string(
        value.get("system"),
        "instructions.system",
        issues,
    )
    if not system_ref:
        return None
    system_path = _validate_repo_file_reference(
        "instructions.system",
        system_ref,
        repo_root,
        issues,
    )
    if system_path is None:
        return None
    return BundleInstructions(system=system_path)


def _parse_runtime(
    value: object,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> BundleRuntime | None:
    if value is None:
        issues.append(ManifestIssue("runtime", "field is required"))
        return None
    if not isinstance(value, Mapping):
        issues.append(ManifestIssue("runtime", "expected a mapping"))
        return None

    _report_unknown_keys(value, RUNTIME_FIELDS, issues, prefix="runtime")
    dockerfile = _coerce_non_empty_string(
        value.get("dockerfile"),
        "runtime.dockerfile",
        issues,
    )
    if not dockerfile:
        return None
    if dockerfile != "Dockerfile":
        issues.append(
            ManifestIssue(
                "runtime.dockerfile",
                "expected exactly 'Dockerfile'",
            )
        )
    dockerfile_path = repo_root / "Dockerfile"
    if not dockerfile_path.is_file():
        issues.append(
            ManifestIssue(
                "runtime.dockerfile",
                "file does not exist at repo root",
            )
        )
        return None
    return BundleRuntime(dockerfile="Dockerfile")


def _parse_sandbox(
    value: object,
    issues: list[ManifestIssue],
) -> BundleSandbox | None:
    if value is None:
        return None
    if not isinstance(value, Mapping):
        issues.append(ManifestIssue("sandbox", "expected a mapping"))
        return None

    _report_unknown_keys(value, SANDBOX_FIELDS, issues, prefix="sandbox")
    provider = _coerce_non_empty_string(
        value.get("provider"),
        "sandbox.provider",
        issues,
    )
    if provider and provider not in SUPPORTED_SANDBOX_PROVIDERS:
        issues.append(
            ManifestIssue(
                "sandbox.provider",
                "expected one of "
                + ", ".join(repr(item) for item in SUPPORTED_SANDBOX_PROVIDERS),
            )
        )
        provider = ""
    provider_api_key_secret = _require_environment_variable_name(
        value.get("provider_api_key_secret"),
        "sandbox.provider_api_key_secret",
        issues,
    )
    if not provider or not provider_api_key_secret:
        return None
    return BundleSandbox(
        provider=provider,  # type: ignore[arg-type]
        provider_api_key_secret=provider_api_key_secret,
    )


def _parse_llm(
    value: object,
    issues: list[ManifestIssue],
) -> BundleLlm | None:
    if value is None:
        return None
    if not isinstance(value, Mapping):
        issues.append(ManifestIssue("llm", "expected a mapping"))
        return None

    _report_unknown_keys(value, LLM_FIELDS, issues, prefix="llm")
    model = _coerce_non_empty_string(value.get("model"), "llm.model", issues)
    base_url = _coerce_non_empty_string(value.get("base_url"), "llm.base_url", issues)
    api_key_secret = _require_environment_variable_name(
        value.get("api_key_secret"),
        "llm.api_key_secret",
        issues,
    )
    if not model or not base_url or not api_key_secret:
        return None
    return BundleLlm(
        model=model,
        base_url=base_url,
        api_key_secret=api_key_secret,
    )


def _require_environment_variable_name(
    value: object,
    field_name: str,
    issues: list[ManifestIssue],
) -> str:
    raw = _coerce_non_empty_string(value, field_name, issues)
    if not raw:
        return ""
    if not ENVIRONMENT_VARIABLE_NAME_PATTERN.fullmatch(raw):
        issues.append(
            ManifestIssue(
                field_name,
                "expected a name matching ^[A-Z_][A-Z0-9_]*$",
            )
        )
        return ""
    return raw


def _validate_llm_references(
    llm: BundleLlm | None,
    secrets: list[str],
    env: Mapping[str, str] | None,
    issues: list[ManifestIssue],
) -> None:
    if llm is None:
        return
    if llm.api_key_secret in secrets:
        issues.append(
            ManifestIssue(
                "llm.api_key_secret",
                "must not also appear in secrets",
            )
        )
    if OPENAI_API_KEY_ENV_NAME in secrets:
        issues.append(
            ManifestIssue(
                "secrets",
                f"must not include {OPENAI_API_KEY_ENV_NAME!r} when llm is configured",
            )
        )
    if env and OPENAI_API_KEY_ENV_NAME in env:
        issues.append(
            ManifestIssue(
                f"env.{OPENAI_API_KEY_ENV_NAME}",
                "must not be set when llm is configured",
            )
        )


def _validate_sandbox_references(
    sandbox: BundleSandbox | None,
    secrets: list[str],
    issues: list[ManifestIssue],
) -> None:
    if sandbox is None:
        return
    if sandbox.provider_api_key_secret in secrets:
        issues.append(
            ManifestIssue(
                "sandbox.provider_api_key_secret",
                "must not also appear in secrets",
            )
        )


def _parse_root_tools(
    value: object,
    issues: list[ManifestIssue],
) -> tuple[RootToolOverride, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        issues.append(ManifestIssue("tools", "expected a list"))
        return ()

    parsed: list[RootToolOverride] = []
    for index, item in enumerate(value):
        label = f"tools[{index}]"
        if not isinstance(item, Mapping):
            issues.append(ManifestIssue(label, "expected an object"))
            continue
        _report_unknown_keys(item, ROOT_TOOL_FIELDS, issues, prefix=label)
        name = _coerce_non_empty_string(item.get("name"), f"{label}.name", issues)
        kind = _coerce_non_empty_string(item.get("kind"), f"{label}.kind", issues)
        if kind and kind not in ROOT_TOOL_KINDS:
            issues.append(
                ManifestIssue(
                    f"{label}.kind",
                    "expected one of 'main' or 'ephemeral'",
                )
            )
        raw_path = item.get("path")
        normalized_path: str | None = None
        if raw_path is not None:
            normalized_path = _normalize_relative_path(
                raw_path,
                label=f"{label}.path",
                issues=issues,
            )
            if normalized_path and not PurePosixPath(normalized_path).is_relative_to(
                PurePosixPath("tools")
            ):
                issues.append(
                    ManifestIssue(
                        f"{label}.path",
                        "expected a path under tools/",
                    )
                )
        parsed.append(
            RootToolOverride(
                name=name,
                kind=(kind if kind in ROOT_TOOL_KINDS else "main"),  # type: ignore[arg-type]
                path=normalized_path,
            )
        )
    return tuple(parsed)


def _discover_custom_tools(
    repo_root: Path,
    issues: list[ManifestIssue],
) -> dict[str, DiscoveredTool]:
    tools_root = repo_root / "tools"
    if not tools_root.exists():
        return {}
    if not tools_root.is_dir():
        issues.append(ManifestIssue("tools", "expected tools/ to be a directory"))
        return {}

    discovered: dict[str, DiscoveredTool] = {}
    for child in sorted(tools_root.iterdir(), key=lambda path: path.name):
        relative_dir = PurePosixPath("tools") / child.name
        label = relative_dir.as_posix()
        if not child.is_dir():
            issues.append(
                ManifestIssue(label, "every immediate tools/ entry must be a directory")
            )
            continue
        tool = _load_discovered_tool(
            tool_dir=child,
            repo_root=repo_root,
            issues=issues,
        )
        if tool is None:
            continue
        discovered[tool.source_path] = tool
    return discovered


def _load_discovered_tool(
    *,
    tool_dir: Path,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> DiscoveredTool | None:
    relative_dir = tool_dir.relative_to(repo_root).as_posix()
    tool_manifest_path = tool_dir / "tool.yml"
    if not tool_manifest_path.is_file():
        issues.append(
            ManifestIssue(f"{relative_dir}.tool.yml", "tool.yml file is required")
        )
        return None

    raw_text = tool_manifest_path.read_text(encoding="utf-8")
    tool_data = _load_yaml_mapping(raw_text, tool_manifest_path)
    _report_unknown_keys(tool_data, TOOL_FIELDS, issues, prefix=f"{relative_dir}.tool")

    source_name = _coerce_non_empty_string(
        tool_data.get("name"),
        f"{relative_dir}.tool.name",
        issues,
    )
    description = _coerce_non_empty_string(
        tool_data.get("description"),
        f"{relative_dir}.tool.description",
        issues,
    )
    runtime_value = _coerce_non_empty_string(
        tool_data.get("runtime"),
        f"{relative_dir}.tool.runtime",
        issues,
    )
    input_schema = _coerce_non_empty_string(
        tool_data.get("input_schema"),
        f"{relative_dir}.tool.input_schema",
        issues,
    )
    handler = _coerce_non_empty_string(
        tool_data.get("handler"),
        f"{relative_dir}.tool.handler",
        issues,
    )
    output_schema_value = tool_data.get("output_schema")
    retries_value = tool_data.get("retries")
    timeout_value = tool_data.get("timeout_seconds")

    if runtime_value and runtime_value not in SUPPORTED_TOOL_RUNTIMES:
        issues.append(
            ManifestIssue(
                f"{relative_dir}.tool.runtime",
                "expected one of "
                + ", ".join(repr(value) for value in SUPPORTED_TOOL_RUNTIMES),
            )
        )

    resolved_input_schema = _validate_tool_file_reference(
        field_name=f"{relative_dir}.tool.input_schema",
        raw_value=input_schema,
        tool_dir=tool_dir,
        repo_root=repo_root,
        issues=issues,
    )
    resolved_output_schema = None
    if output_schema_value is not None:
        output_schema = _coerce_non_empty_string(
            output_schema_value,
            f"{relative_dir}.tool.output_schema",
            issues,
        )
        if output_schema:
            resolved_output_schema = _validate_tool_file_reference(
                field_name=f"{relative_dir}.tool.output_schema",
                raw_value=output_schema,
                tool_dir=tool_dir,
                repo_root=repo_root,
                issues=issues,
            )

    resolved_handler = _validate_handler_reference(
        field_name=f"{relative_dir}.tool.handler",
        raw_value=handler,
        tool_dir=tool_dir,
        repo_root=repo_root,
        issues=issues,
    )
    retries = _optional_positive_int(
        f"{relative_dir}.tool.retries",
        retries_value,
        issues,
    )
    timeout_seconds = _optional_positive_int(
        f"{relative_dir}.tool.timeout_seconds",
        timeout_value,
        issues,
    )

    if not (
        source_name
        and description
        and runtime_value in SUPPORTED_TOOL_RUNTIMES
        and resolved_input_schema
        and resolved_handler
    ):
        return None

    return DiscoveredTool(
        source_name=source_name,
        source_path=relative_dir,
        description=description,
        input_schema=resolved_input_schema,
        output_schema=resolved_output_schema,
        runtime=runtime_value,  # type: ignore[arg-type]
        handler=resolved_handler,
        retries=retries,
        timeout_seconds=timeout_seconds,
    )


def _build_effective_tools(
    *,
    root_tools: tuple[RootToolOverride, ...],
    discovered_tools: Mapping[str, DiscoveredTool],
    issues: list[ManifestIssue],
) -> tuple[list[BuiltInTool], list[CustomTool]]:
    built_in_tools: list[BuiltInTool] = []
    overrides_by_path: dict[str, RootToolOverride] = {}
    for index, entry in enumerate(root_tools):
        label = f"tools[{index}]"
        if entry.path is None:
            built_in_tools.append(
                BuiltInTool(
                    name=entry.name,
                    execution_mode=entry.kind,
                )
            )
            continue
        if entry.path not in discovered_tools:
            issues.append(
                ManifestIssue(
                    f"{label}.path",
                    "did not match a discovered tools/<name>/ directory",
                )
            )
            continue
        if entry.path in overrides_by_path:
            issues.append(
                ManifestIssue(
                    f"{label}.path",
                    "multiple root tool entries cannot target the same tool path",
                )
            )
            continue
        overrides_by_path[entry.path] = entry

    custom_tools: list[CustomTool] = []
    for source_path, tool in sorted(discovered_tools.items()):
        override = overrides_by_path.get(source_path)
        custom_tools.append(
            CustomTool(
                name=tool.source_name if override is None else override.name,
                source_name=tool.source_name,
                source_path=tool.source_path,
                description=tool.description,
                input_schema=tool.input_schema,
                output_schema=tool.output_schema,
                runtime=tool.runtime,
                handler=tool.handler,
                retries=tool.retries,
                timeout_seconds=tool.timeout_seconds,
                execution_mode="client" if override is None else override.kind,
            )
        )

    _report_duplicate_tool_names(built_in_tools, custom_tools, issues)
    return built_in_tools, custom_tools


def _report_duplicate_tool_names(
    built_in_tools: list[BuiltInTool],
    custom_tools: list[CustomTool],
    issues: list[ManifestIssue],
) -> None:
    seen: dict[str, str] = {}
    for index, tool in enumerate(built_in_tools):
        previous = seen.setdefault(tool.name, f"built_in_tools[{index}]")
        if previous != f"built_in_tools[{index}]":
            issues.append(
                ManifestIssue(
                    f"built_in_tools[{index}].name",
                    f"duplicate tool name {tool.name!r}",
                )
            )
    for index, tool in enumerate(custom_tools):
        previous = seen.setdefault(tool.name, f"custom_tools[{index}]")
        if previous != f"custom_tools[{index}]":
            issues.append(
                ManifestIssue(
                    f"custom_tools[{index}].name",
                    f"duplicate tool name {tool.name!r}",
                )
            )


def _validate_repo_file_reference(
    field_name: str,
    raw_value: str,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> str | None:
    normalized_path = _normalize_relative_path(
        raw_value,
        label=field_name,
        issues=issues,
    )
    if normalized_path is None:
        return None
    target_path = repo_root / normalized_path
    if not target_path.is_file():
        issues.append(ManifestIssue(field_name, "file does not exist"))
        return None
    return normalized_path


def _validate_tool_file_reference(
    *,
    field_name: str,
    raw_value: str,
    tool_dir: Path,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> str | None:
    normalized_local = _normalize_relative_path(
        raw_value,
        label=field_name,
        issues=issues,
    )
    if normalized_local is None:
        return None
    target_path = tool_dir / normalized_local
    if not target_path.is_file():
        issues.append(ManifestIssue(field_name, "file does not exist"))
        return None
    return target_path.relative_to(repo_root).as_posix()


def _validate_handler_reference(
    *,
    field_name: str,
    raw_value: str,
    tool_dir: Path,
    repo_root: Path,
    issues: list[ManifestIssue],
) -> str | None:
    module_path, separator, export_name = raw_value.partition(":")
    if (
        separator != ":"
        or not module_path.strip()
        or not export_name.strip()
        or ":" in export_name
    ):
        issues.append(
            ManifestIssue(
                field_name,
                "expected a '<module-or-path>:<export>' reference",
            )
        )
        return None
    normalized_module = _normalize_relative_path(
        module_path,
        label=field_name,
        issues=issues,
    )
    if normalized_module is None:
        return None
    target_path = tool_dir / normalized_module
    if not target_path.is_file():
        issues.append(ManifestIssue(field_name, "handler target file does not exist"))
        return None
    repo_relative = target_path.relative_to(repo_root).as_posix()
    return f"{repo_relative}:{export_name.strip()}"


def _normalize_relative_path(
    value: object,
    *,
    label: str,
    issues: list[ManifestIssue],
) -> str | None:
    if not isinstance(value, str) or not value.strip():
        issues.append(ManifestIssue(label, "expected a non-empty relative path"))
        return None
    candidate = PurePosixPath(value.strip())
    if candidate.is_absolute():
        issues.append(ManifestIssue(label, "expected a relative path"))
        return None
    if not candidate.parts or any(part in {"", ".", ".."} for part in candidate.parts):
        issues.append(
            ManifestIssue(
                label,
                "path must not contain '.', '..', or empty segments",
            )
        )
        return None
    return candidate.as_posix()


def _optional_positive_int(
    field_name: str,
    value: object,
    issues: list[ManifestIssue],
) -> int | None:
    if value is None:
        return None
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        issues.append(ManifestIssue(field_name, "expected a positive integer"))
        return None
    return value


def _parse_env(value: Any, issues: list[ManifestIssue]) -> dict[str, str] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        issues.append(ManifestIssue("env", "expected a mapping"))
        return None

    env: dict[str, str] = {}
    for raw_name, raw_value in value.items():
        if not isinstance(raw_name, str):
            issues.append(ManifestIssue("env", "expected string keys"))
            continue
        name = raw_name.strip()
        if not ENVIRONMENT_VARIABLE_NAME_PATTERN.fullmatch(name):
            issues.append(
                ManifestIssue(
                    f"env.{raw_name}",
                    "expected a name matching ^[A-Z_][A-Z0-9_]*$",
                )
            )
            continue
        if not isinstance(raw_value, str):
            issues.append(
                ManifestIssue(
                    f"env.{raw_name}",
                    "expected a string value",
                )
            )
            continue
        env[name] = raw_value
    return env


def _parse_secrets(value: Any, issues: list[ManifestIssue]) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list):
        issues.append(
            ManifestIssue(
                "secrets",
                "expected a list of secret names; secret values must not live in the manifest",
            )
        )
        return []

    secrets: list[str] = []
    seen: set[str] = set()
    for index, item in enumerate(value):
        if not isinstance(item, str) or not ENVIRONMENT_VARIABLE_NAME_PATTERN.fullmatch(
            item.strip()
        ):
            issues.append(
                ManifestIssue(
                    f"secrets[{index}]",
                    "expected a secret name matching ^[A-Z_][A-Z0-9_]*$",
                )
            )
            continue
        name = item.strip()
        if name in seen:
            issues.append(
                ManifestIssue(
                    f"secrets[{index}]",
                    f"duplicate secret name {name!r}",
                )
            )
            continue
        seen.add(name)
        secrets.append(name)
    return secrets


def _report_secret_env_overlap(
    secrets: list[str],
    env: Mapping[str, str] | None,
    issues: list[ManifestIssue],
) -> None:
    if not env:
        return
    overlap = sorted(set(secrets) & set(env))
    if overlap:
        issues.append(
            ManifestIssue(
                "secrets",
                "secret names must not overlap env keys: " + ", ".join(overlap),
            )
        )

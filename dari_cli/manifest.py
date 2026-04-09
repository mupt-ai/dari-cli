"""Manifest parsing and validation for repo-root ``dari.yml`` files."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal, Mapping

import yaml

SUPPORTED_SDKS = (
    "openai-agents",
    "claude-agent-sdk",
    "opencode",
    "pi",
)
TOP_LEVEL_FIELDS = {
    "name",
    "sdk",
    "entrypoint",
    "runtime",
    "retries",
    "secrets",
    "env",
}
RUNTIME_FIELDS = {"language", "version", "timeout_seconds"}
RETRIES_FIELDS = {"max_attempts"}


@dataclass(frozen=True)
class ManifestIssue:
    """A single validation issue for the manifest."""

    path: str
    message: str


@dataclass(frozen=True)
class ManifestRuntime:
    """Optional runtime settings from the manifest."""

    language: str | None = None
    version: str | None = None
    timeout_seconds: int | None = None

    def to_dict(self) -> dict[str, Any]:
        """Return the runtime payload without unset fields."""
        payload: dict[str, Any] = {}
        if self.language is not None:
            payload["language"] = self.language
        if self.version is not None:
            payload["version"] = self.version
        if self.timeout_seconds is not None:
            payload["timeout_seconds"] = self.timeout_seconds
        return payload


@dataclass(frozen=True)
class RetryPolicy:
    """Optional retry configuration from the manifest."""

    max_attempts: int | None = None

    def to_dict(self) -> dict[str, Any]:
        """Return the retry payload without unset fields."""
        payload: dict[str, Any] = {}
        if self.max_attempts is not None:
            payload["max_attempts"] = self.max_attempts
        return payload


@dataclass(frozen=True)
class AgentManifest:
    """Validated manifest values."""

    name: str
    sdk: Literal["openai-agents", "claude-agent-sdk", "opencode", "pi"]
    entrypoint: str
    runtime: ManifestRuntime | None = None
    retries: RetryPolicy | None = None
    secrets: tuple[str, ...] = ()
    env: Mapping[str, str] | None = None

    def to_dict(self) -> dict[str, Any]:
        """Return the manifest as a JSON-serializable payload."""
        payload: dict[str, Any] = {
            "name": self.name,
            "sdk": self.sdk,
            "entrypoint": self.entrypoint,
        }
        if self.runtime is not None:
            runtime_payload = self.runtime.to_dict()
            if runtime_payload:
                payload["runtime"] = runtime_payload
        if self.retries is not None:
            retry_payload = self.retries.to_dict()
            if retry_payload:
                payload["retries"] = retry_payload
        if self.secrets:
            payload["secrets"] = list(self.secrets)
        if self.env:
            payload["env"] = dict(self.env)
        return payload


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
    """Load and validate the repo-root ``dari.yml`` manifest."""
    manifest_path = Path(repo_root) / "dari.yml"
    raw_text = manifest_path.read_text(encoding="utf-8")
    return parse_manifest_text(raw_text, manifest_path)


def parse_manifest_text(raw_text: str, manifest_path: str | Path = "dari.yml") -> AgentManifest:
    """Parse and validate manifest text."""
    resolved_path = Path(manifest_path)

    try:
        loaded = yaml.safe_load(raw_text)
    except yaml.YAMLError as exc:
        raise ManifestValidationError(
            resolved_path,
            [ManifestIssue("manifest", f"invalid YAML: {exc}")],
        ) from exc

    if loaded is None and yaml.compose(raw_text) is None:
        loaded = {}

    if not isinstance(loaded, dict):
        raise ManifestValidationError(
            resolved_path,
            [ManifestIssue("manifest", "expected a top-level mapping")],
        )

    issues: list[ManifestIssue] = []
    data = dict(loaded)
    _report_unknown_keys(data, TOP_LEVEL_FIELDS, issues)

    name = _require_non_empty_string(data, "name", issues)
    sdk = _require_sdk(data, issues)
    entrypoint = _require_entrypoint(data, issues)
    runtime = _parse_runtime(data.get("runtime"), issues)
    retries = _parse_retries(data.get("retries"), issues)
    secrets = _parse_secrets(data.get("secrets"), issues)
    env = _parse_env(data.get("env"), issues)

    if issues:
        raise ManifestValidationError(resolved_path, issues)

    return AgentManifest(
        name=name,
        sdk=sdk,
        entrypoint=entrypoint,
        runtime=runtime,
        retries=retries,
        secrets=tuple(secrets),
        env=env,
    )


def _report_unknown_keys(
    mapping: Mapping[str, Any], allowed: set[str], issues: list[ManifestIssue], prefix: str = ""
) -> None:
    for key in sorted(mapping, key=str):
        if key not in allowed:
            key_text = str(key)
            path = f"{prefix}.{key_text}" if prefix else key_text
            issues.append(ManifestIssue(path, "unsupported field"))


def _require_non_empty_string(
    data: Mapping[str, Any], field_name: str, issues: list[ManifestIssue]
) -> str:
    value = data.get(field_name)
    if value is None:
        issues.append(ManifestIssue(field_name, "field is required"))
        return ""
    if not isinstance(value, str) or not value.strip():
        issues.append(ManifestIssue(field_name, "expected a non-empty string"))
        return ""
    return value.strip()


def _require_sdk(
    data: Mapping[str, Any], issues: list[ManifestIssue]
) -> Literal["openai-agents", "claude-agent-sdk", "opencode", "pi"]:
    raw_sdk = _require_non_empty_string(data, "sdk", issues)
    if not raw_sdk:
        return "openai-agents"
    if raw_sdk not in SUPPORTED_SDKS:
        issues.append(
            ManifestIssue(
                "sdk",
                "expected one of "
                + ", ".join(repr(value) for value in SUPPORTED_SDKS),
            )
        )
        return "openai-agents"
    return raw_sdk  # type: ignore[return-value]


def _require_entrypoint(data: Mapping[str, Any], issues: list[ManifestIssue]) -> str:
    entrypoint = _require_non_empty_string(data, "entrypoint", issues)
    if entrypoint and not _is_valid_entrypoint_reference(entrypoint):
        issues.append(
            ManifestIssue(
                "entrypoint",
                "expected a '<module-or-path>:<export>' reference",
            )
        )
    return entrypoint


def _is_valid_entrypoint_reference(entrypoint: str) -> bool:
    module_or_path, separator, export_name = entrypoint.partition(":")
    return (
        separator == ":"
        and bool(module_or_path.strip())
        and bool(export_name.strip())
        and ":" not in export_name
    )


def _parse_runtime(value: Any, issues: list[ManifestIssue]) -> ManifestRuntime | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        issues.append(ManifestIssue("runtime", "expected a mapping"))
        return None

    _report_unknown_keys(value, RUNTIME_FIELDS, issues, prefix="runtime")
    language = _optional_non_empty_string(
        "runtime.language",
        value.get("language"),
        issues,
    )
    version = _optional_non_empty_string(
        "runtime.version",
        value.get("version"),
        issues,
    )
    parsed_timeout = _optional_positive_int(
        "runtime.timeout_seconds",
        value.get("timeout_seconds"),
        issues,
    )

    return ManifestRuntime(
        language=language,
        version=version,
        timeout_seconds=parsed_timeout,
    )


def _parse_retries(value: Any, issues: list[ManifestIssue]) -> RetryPolicy | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        issues.append(ManifestIssue("retries", "expected a mapping"))
        return None

    _report_unknown_keys(value, RETRIES_FIELDS, issues, prefix="retries")
    parsed_attempts = _optional_positive_int(
        "retries.max_attempts",
        value.get("max_attempts"),
        issues,
    )

    return RetryPolicy(max_attempts=parsed_attempts)


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

    parsed: list[str] = []
    for index, item in enumerate(value):
        path = f"secrets[{index}]"
        if not isinstance(item, str) or not item.strip():
            issues.append(ManifestIssue(path, "expected a non-empty secret name"))
            continue
        parsed.append(item.strip())
    return parsed


def _parse_env(value: Any, issues: list[ManifestIssue]) -> dict[str, str] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        issues.append(ManifestIssue("env", "expected a mapping of string values"))
        return None

    parsed: dict[str, str] = {}
    for key, item in value.items():
        path = f"env.{key}"
        if not isinstance(key, str) or not key.strip():
            issues.append(ManifestIssue(path, "expected a non-empty environment variable name"))
            continue
        if not isinstance(item, str):
            issues.append(ManifestIssue(path, "expected a string value"))
            continue
        parsed[key] = item
    return parsed


def _optional_non_empty_string(
    path: str,
    value: Any,
    issues: list[ManifestIssue],
) -> str | None:
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        issues.append(ManifestIssue(path, "expected a non-empty string"))
        return None
    return value.strip()


def _optional_positive_int(
    path: str,
    value: Any,
    issues: list[ManifestIssue],
) -> int | None:
    if value is None:
        return None
    if type(value) is not int or value <= 0:
        issues.append(ManifestIssue(path, "expected a positive integer"))
        return None
    return value

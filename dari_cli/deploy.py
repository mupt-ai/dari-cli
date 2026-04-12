"""Checkout packaging and deploy orchestration for the Dari CLI."""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Mapping
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from .manifest import load_manifest
from .source_bundles import (
    SourceBundleMetadataValue,
    build_source_bundle_archive,
    collect_source_bundle_metadata,
)

DEPLOY_STATE_DIRNAME = ".dari"
DEPLOY_STATE_FILENAME = "deploy-state.json"
DEPLOY_STATE_SCHEMA_VERSION = 1
DEPLOY_STATE_KEY = "agents"
SOURCE_SNAPSHOTS_ENDPOINT = "/v1/source-snapshots"
SOURCE_SNAPSHOT_ID_PLACEHOLDER = "<source_snapshot_id from reserve step>"
SIGNED_UPLOAD_URL_PLACEHOLDER = "<signed upload URL from reserve step>"
UPLOAD_HEADERS_PLACEHOLDER = "<upload_headers from reserve step>"


@dataclass(frozen=True)
class SourceBundle:
    """Packaged checkout content plus metadata used for publishing."""

    content: bytes
    sha256: str
    size_bytes: int
    file_count: int
    metadata: Mapping[str, SourceBundleMetadataValue] | None = None

    def to_snapshot_payload(self) -> dict[str, Any]:
        """Render the bundle as a source snapshot reservation payload."""
        payload: dict[str, Any] = {
            "format": "tar.gz",
            "sha256": self.sha256,
            "size_bytes": self.size_bytes,
        }
        if self.metadata:
            payload["metadata"] = dict(self.metadata)
        return payload


@dataclass(frozen=True)
class PreparedDeployFlow:
    """The local deploy flow for dry runs and live publishes."""

    bundle: SourceBundle
    publish_endpoint: str
    manifest_payload: Mapping[str, Any]
    agent_name: str | None = None
    execution_backend_id: str | None = None

    def build_publish_payload(self, source_snapshot_id: str) -> dict[str, Any]:
        """Build the final publish payload using a reserved snapshot ID."""
        payload: dict[str, Any] = {
            "manifest": dict(self.manifest_payload),
            "source_snapshot_id": source_snapshot_id,
        }
        if self.agent_name is not None:
            payload["name"] = self.agent_name
        if self.execution_backend_id is not None:
            payload["execution_backend_id"] = self.execution_backend_id
        return payload

    def to_dict(self) -> dict[str, Any]:
        """Render the deploy flow for `--dry-run` output."""
        return {
            "bundle": {
                "file_count": self.bundle.file_count,
                "sha256": self.bundle.sha256,
                "size_bytes": self.bundle.size_bytes,
                "metadata": (
                    None if self.bundle.metadata is None else dict(self.bundle.metadata)
                ),
            },
            "manifest": dict(self.manifest_payload),
            "steps": [
                {
                    "action": "reserve_source_snapshot",
                    "method": "POST",
                    "endpoint": SOURCE_SNAPSHOTS_ENDPOINT,
                    "payload": self.bundle.to_snapshot_payload(),
                },
                {
                    "action": "upload_source_bundle",
                    "method": "PUT",
                    "upload_url": SIGNED_UPLOAD_URL_PLACEHOLDER,
                    "headers": UPLOAD_HEADERS_PLACEHOLDER,
                    "size_bytes": self.bundle.size_bytes,
                    "sha256": self.bundle.sha256,
                },
                {
                    "action": "finalize_source_snapshot",
                    "method": "POST",
                    "endpoint": build_finalize_source_snapshot_endpoint(
                        SOURCE_SNAPSHOT_ID_PLACEHOLDER
                    ),
                },
                {
                    "action": "publish_agent_version"
                    if self.agent_name is None
                    else "create_agent",
                    "method": "POST",
                    "endpoint": self.publish_endpoint,
                    "payload": self.build_publish_payload(
                        SOURCE_SNAPSHOT_ID_PLACEHOLDER
                    ),
                },
            ]
        }


class DariApiError(RuntimeError):
    """Raised when the Dari API rejects a deploy request."""


class DeployConfigurationError(RuntimeError):
    """Raised when local deploy inputs are incompatible with the publish API."""


@dataclass(frozen=True)
class DariApiClient:
    """Small HTTP client for the Dari deploy endpoints."""

    api_url: str
    api_key: str
    opener: Callable[..., Any] = urlopen

    def deploy_checkout(
        self,
        repo_root: str | Path,
        *,
        agent_id: str | None = None,
        execution_backend_id: str | None = None,
        environ: Mapping[str, str] | None = None,
    ) -> dict[str, Any]:
        """Package the checkout and submit it through the configured client."""
        return deploy_checkout(
            repo_root,
            api_url=self.api_url,
            api_key=self.api_key,
            agent_id=agent_id,
            execution_backend_id=execution_backend_id,
            environ=environ,
            opener=self.opener,
        )


def collect_source_metadata(
    repo_root: str | Path,
    *,
    environ: Mapping[str, str] | None = None,
) -> dict[str, SourceBundleMetadataValue]:
    """Collect optional source metadata from the checkout and environment."""
    return collect_source_bundle_metadata(Path(repo_root), environ=environ)


def build_publish_endpoint(agent_id: str | None = None) -> str:
    """Return the publish endpoint for a new agent or an existing one."""
    if agent_id:
        return f"/v1/agents/{agent_id}/versions"
    return "/v1/agents"


def build_finalize_source_snapshot_endpoint(source_snapshot_id: str) -> str:
    """Return the finalize endpoint for a reserved source snapshot."""
    return f"{SOURCE_SNAPSHOTS_ENDPOINT}/{source_snapshot_id}/finalize"


def build_delete_source_snapshot_endpoint(source_snapshot_id: str) -> str:
    """Return the delete endpoint for a reserved source snapshot."""
    return f"{SOURCE_SNAPSHOTS_ENDPOINT}/{source_snapshot_id}"


def build_source_bundle(
    repo_root: str | Path,
    *,
    metadata: Mapping[str, SourceBundleMetadataValue] | None = None,
) -> SourceBundle:
    """Create a deterministic source bundle for the checkout."""
    built_archive = build_source_bundle_archive(Path(repo_root))
    return SourceBundle(
        content=built_archive.content,
        sha256=built_archive.archive_sha256,
        size_bytes=len(built_archive.content),
        file_count=len(built_archive.included_paths),
        metadata=metadata,
    )


def prepare_deploy_flow(
    repo_root: str | Path,
    *,
    agent_id: str | None = None,
    execution_backend_id: str | None = None,
    api_url: str | None = None,
    environ: Mapping[str, str] | None = None,
) -> PreparedDeployFlow:
    """Build the local deploy flow before any network calls."""
    resolved_agent_id = _resolve_agent_id(
        repo_root,
        agent_id=agent_id,
        api_url=api_url,
    )
    manifest = load_manifest(repo_root)
    resolved_execution_backend_id = _normalize_optional_string(execution_backend_id)
    _validate_execution_backend_selection(
        harness=manifest.harness,
        execution_backend_id=resolved_execution_backend_id,
    )
    source_metadata = collect_source_metadata(repo_root, environ=environ)
    bundle = build_source_bundle(
        repo_root,
        metadata=source_metadata or None,
    )
    return PreparedDeployFlow(
        bundle=bundle,
        publish_endpoint=build_publish_endpoint(resolved_agent_id),
        manifest_payload=manifest.to_dict(),
        agent_name=None if resolved_agent_id else manifest.name,
        execution_backend_id=resolved_execution_backend_id,
    )


def deploy_checkout(
    repo_root: str | Path,
    *,
    api_url: str,
    api_key: str,
    agent_id: str | None = None,
    execution_backend_id: str | None = None,
    environ: Mapping[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Package the checkout, upload it, and publish it."""
    resolved_repo_root = Path(repo_root).resolve()
    prepared = prepare_deploy_flow(
        resolved_repo_root,
        agent_id=agent_id,
        execution_backend_id=execution_backend_id,
        api_url=api_url,
        environ=environ,
    )

    reserved_snapshot = _api_json_request(
        api_url,
        SOURCE_SNAPSHOTS_ENDPOINT,
        api_key=api_key,
        payload=prepared.bundle.to_snapshot_payload(),
        opener=opener,
    )
    source_snapshot_id = _require_string(
        reserved_snapshot,
        "source_snapshot_id",
        context="source snapshot reserve response",
    )
    upload_url = _require_string(
        reserved_snapshot,
        "upload_url",
        context="source snapshot reserve response",
    )
    upload_headers = _require_string_mapping(
        reserved_snapshot.get("upload_headers"),
        context="source snapshot reserve response",
    )

    _upload_bundle(
        upload_url,
        prepared.bundle.content,
        headers=upload_headers,
        opener=opener,
    )

    finalized_snapshot = _api_json_request(
        api_url,
        build_finalize_source_snapshot_endpoint(source_snapshot_id),
        api_key=api_key,
        payload=None,
        opener=opener,
    )
    finalize_status = _require_string(
        finalized_snapshot,
        "status",
        context="source snapshot finalize response",
    )
    if finalize_status != "ready":
        failure_reason = finalized_snapshot.get("failure_reason")
        detail = (
            str(failure_reason)
            if isinstance(failure_reason, str) and failure_reason.strip()
            else "Source snapshot failed verification."
        )
        raise DariApiError(
            f"Source snapshot {source_snapshot_id} did not become ready: {detail}"
        )

    try:
        response_json = _api_json_request(
            api_url,
            prepared.publish_endpoint,
            api_key=api_key,
            payload=prepared.build_publish_payload(source_snapshot_id),
            opener=opener,
        )
    except DariApiError as publish_error:
        cleanup_error = _delete_source_snapshot_best_effort(
            api_url,
            source_snapshot_id,
            api_key=api_key,
            opener=opener,
        )
        if cleanup_error is not None:
            raise DariApiError(
                "Publishing failed after snapshot finalize and cleanup also failed "
                f"for source snapshot {source_snapshot_id}: {publish_error}; "
                f"cleanup error: {cleanup_error}"
            ) from publish_error
        raise

    _persist_agent_id_if_present(
        resolved_repo_root,
        api_url=api_url,
        response_payload=response_json,
    )
    return response_json


def _api_json_request(
    api_url: str,
    endpoint: str,
    *,
    api_key: str,
    payload: Mapping[str, Any] | None,
    opener: Callable[..., Any],
    method: str = "POST",
) -> dict[str, Any]:
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {
        "Authorization": f"Bearer {api_key}",
    }
    if payload is not None:
        headers["Content-Type"] = "application/json"
    request = Request(
        url=f"{api_url.rstrip('/')}{endpoint}",
        data=data,
        headers=headers,
        method=method,
    )
    return _read_json_response(request, opener=opener, context=endpoint)


def _delete_source_snapshot_best_effort(
    api_url: str,
    source_snapshot_id: str,
    *,
    api_key: str,
    opener: Callable[..., Any],
) -> DariApiError | None:
    try:
        _api_json_request(
            api_url,
            build_delete_source_snapshot_endpoint(source_snapshot_id),
            api_key=api_key,
            payload=None,
            opener=opener,
            method="DELETE",
        )
    except DariApiError as exc:
        return exc
    return None


def _upload_bundle(
    upload_url: str,
    bundle_content: bytes,
    *,
    headers: Mapping[str, str],
    opener: Callable[..., Any],
) -> None:
    request = Request(
        url=upload_url,
        data=bundle_content,
        headers=dict(headers),
        method="PUT",
    )
    try:
        with opener(request) as response:
            response.read()
    except HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise DariApiError(
            f"Bundle upload failed with {exc.code}: {error_body}"
        ) from exc


def _read_json_response(
    request: Request,
    *,
    opener: Callable[..., Any],
    context: str,
) -> dict[str, Any]:
    try:
        with opener(request) as response:
            response_body = response.read().decode("utf-8")
    except HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise DariApiError(
            f"Dari API request failed for {context} with {exc.code}: {error_body}"
        ) from exc

    if not response_body:
        return {}
    try:
        response_json = json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise DariApiError(
            f"Dari API response for {context} was not valid JSON."
        ) from exc
    if not isinstance(response_json, dict):
        raise DariApiError(f"Dari API response for {context} was not a JSON object.")
    return response_json


def _require_string(
    payload: Mapping[str, Any],
    field_name: str,
    *,
    context: str,
) -> str:
    raw_value = payload.get(field_name)
    if not isinstance(raw_value, str) or not raw_value.strip():
        raise DariApiError(f"{context} is missing a valid {field_name!r} field.")
    return raw_value.strip()


def _require_string_mapping(
    raw_value: object,
    *,
    context: str,
) -> dict[str, str]:
    if raw_value is None:
        return {}
    if not isinstance(raw_value, Mapping):
        raise DariApiError(f"{context} contained invalid upload headers.")

    headers: dict[str, str] = {}
    for key, value in raw_value.items():
        if not isinstance(key, str) or not isinstance(value, str):
            raise DariApiError(f"{context} contained invalid upload headers.")
        headers[key] = value
    return headers


def _normalize_optional_string(value: str | None) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    return normalized or None


def _validate_execution_backend_selection(
    *,
    harness: str,
    execution_backend_id: str | None,
) -> None:
    if harness == "pi":
        if execution_backend_id is None:
            raise DeployConfigurationError(
                "execution_backend_id is required for harness 'pi'. "
                "Pass --execution-backend-id or set DARI_EXECUTION_BACKEND_ID."
            )
        return
    if execution_backend_id is not None:
        raise DeployConfigurationError(
            "execution_backend_id is only supported for harness 'pi'. "
            "Remove --execution-backend-id or unset DARI_EXECUTION_BACKEND_ID."
        )


def _resolve_agent_id(
    repo_root: str | Path,
    *,
    agent_id: str | None,
    api_url: str | None,
) -> str | None:
    if agent_id:
        return agent_id
    if not api_url:
        return None
    state = _load_deploy_state(Path(repo_root).resolve())
    return state.get(_normalize_api_url(api_url))


def _persist_agent_id_if_present(
    repo_root: Path,
    *,
    api_url: str,
    response_payload: Mapping[str, Any],
) -> None:
    raw_agent_id = response_payload.get("agent_id", response_payload.get("id"))
    if not isinstance(raw_agent_id, str):
        return
    agent_id = raw_agent_id.strip()
    if not agent_id:
        return
    _save_deploy_state(
        repo_root,
        api_url=api_url,
        agent_id=agent_id,
    )


def _load_deploy_state(repo_root: Path) -> dict[str, str]:
    path = _deploy_state_path(repo_root)
    if not path.exists():
        return {}

    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"Deploy state file must contain an object: {path}")
    version = payload.get("version")
    if version != DEPLOY_STATE_SCHEMA_VERSION:
        raise ValueError(f"Unsupported deploy state schema version in {path}")
    raw_agents = payload.get(DEPLOY_STATE_KEY, {})
    if not isinstance(raw_agents, dict):
        raise ValueError(f"Deploy state agents map must be an object: {path}")

    agents: dict[str, str] = {}
    for raw_api_url, raw_agent_id in raw_agents.items():
        if not isinstance(raw_api_url, str) or not isinstance(raw_agent_id, str):
            raise ValueError(f"Deploy state entries must be string pairs: {path}")
        normalized_api_url = _normalize_api_url(raw_api_url)
        agent_id = raw_agent_id.strip()
        if normalized_api_url and agent_id:
            agents[normalized_api_url] = agent_id
    return agents


def _save_deploy_state(
    repo_root: Path,
    *,
    api_url: str,
    agent_id: str,
) -> None:
    normalized_api_url = _normalize_api_url(api_url)
    state = _load_deploy_state(repo_root)
    state[normalized_api_url] = agent_id

    path = _deploy_state_path(repo_root)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(
            {
                "version": DEPLOY_STATE_SCHEMA_VERSION,
                DEPLOY_STATE_KEY: state,
            },
            indent=2,
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )


def _deploy_state_path(repo_root: Path) -> Path:
    return repo_root / DEPLOY_STATE_DIRNAME / DEPLOY_STATE_FILENAME


def _normalize_api_url(api_url: str) -> str:
    return api_url.rstrip("/")

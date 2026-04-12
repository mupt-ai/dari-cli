from __future__ import annotations

import io
import json
import shutil
import subprocess
import tarfile
from pathlib import Path
from urllib.error import HTTPError

import pytest

from dari_cli.__main__ import main
from dari_cli.deploy import (
    DariApiError,
    build_source_bundle,
    collect_source_metadata,
    deploy_checkout,
    prepare_deploy_flow,
)


def write_valid_bundle(repo_root: Path) -> None:
    (repo_root / "prompts").mkdir(parents=True, exist_ok=True)
    (repo_root / "prompts" / "system.md").write_text(
        "You are a managed test bundle.\n",
        encoding="utf-8",
    )
    (repo_root / "Dockerfile").write_text(
        "FROM python:3.11-slim\nWORKDIR /bundle\nCOPY . /bundle\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search").mkdir(parents=True, exist_ok=True)
    (repo_root / "tools" / "repo_search" / "tool.yml").write_text(
        "\n".join(
            [
                "name: repo_search",
                "description: Search the repository for matching content.",
                "input_schema: input.schema.json",
                "runtime: python",
                "handler: handler.py:main",
                "retries: 2",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search" / "handler.py").write_text(
        "def main(payload):\n    return payload\n",
        encoding="utf-8",
    )
    (repo_root / "tools" / "repo_search" / "input.schema.json").write_text(
        '{"type":"object","properties":{"query":{"type":"string"}}}',
        encoding="utf-8",
    )
    (repo_root / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "harness: openai-agents",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "tools:",
                "  - name: repo_search",
                "    path: tools/repo_search",
                "    kind: main",
                "  - name: sandbox.exec",
                "    kind: ephemeral",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def _git_available() -> bool:
    return shutil.which("git") is not None


def _init_git_repo(tmp_path: Path) -> str:
    subprocess.run(["git", "init"], cwd=tmp_path, check=True)
    subprocess.run(
        ["git", "config", "user.email", "test@example.com"],
        cwd=tmp_path,
        check=True,
    )
    subprocess.run(
        ["git", "config", "user.name", "Test User"],
        cwd=tmp_path,
        check=True,
    )
    subprocess.run(["git", "branch", "-M", "main"], cwd=tmp_path, check=True)
    subprocess.run(["git", "add", "."], cwd=tmp_path, check=True)
    subprocess.run(["git", "commit", "-m", "initial"], cwd=tmp_path, check=True)
    return subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=tmp_path,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def test_build_source_bundle_packages_managed_bundle_and_skips_git_metadata(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / ".git").mkdir()
    (tmp_path / ".git" / "config").write_text("[core]\n", encoding="utf-8")
    (tmp_path / ".venv").mkdir()
    (tmp_path / ".venv" / "ignore.txt").write_text("ignored\n", encoding="utf-8")

    bundle = build_source_bundle(tmp_path)

    assert bundle.file_count == 6
    assert bundle.size_bytes > 0
    assert bundle.sha256

    with tarfile.open(fileobj=io.BytesIO(bundle.content), mode="r:gz") as archive:
        names = sorted(archive.getnames())

    assert names == [
        "Dockerfile",
        "dari.yml",
        "prompts/system.md",
        "tools/repo_search/handler.py",
        "tools/repo_search/input.schema.json",
        "tools/repo_search/tool.yml",
    ]


@pytest.mark.skipif(not _git_available(), reason="git is required for this test")
def test_collect_source_metadata_includes_git_provenance(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    commit_sha = _init_git_repo(tmp_path)
    (tmp_path / "Dockerfile").write_text("FROM python:3.12-slim\n", encoding="utf-8")

    metadata = collect_source_metadata(tmp_path, environ={})

    assert metadata == {
        "git_commit_sha": commit_sha,
        "git_dirty": True,
        "git_ref": "main",
        "origin": "local_cli",
    }


def test_prepare_deploy_flow_outputs_normalized_manifest_and_steps(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)

    prepared = prepare_deploy_flow(
        tmp_path,
        environ={"CI": "true", "GITHUB_SHA": "deadbeef"},
    )
    dry_run = prepared.to_dict()

    assert dry_run["bundle"]["metadata"] == {
        "git_commit_sha": "deadbeef",
        "origin": "ci",
    }
    assert dry_run["manifest"]["harness"] == "openai-agents"
    assert dry_run["manifest"]["built_in_tools"] == [
        {"execution_mode": "ephemeral", "name": "sandbox.exec"}
    ]
    assert dry_run["manifest"]["custom_tools"][0]["execution_mode"] == "main"
    assert dry_run["steps"][0]["endpoint"] == "/v1/source-snapshots"
    assert dry_run["steps"][3]["endpoint"] == "/v1/agents"


def test_deploy_checkout_reserves_uploads_finalizes_and_publishes(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    captured_requests: list[dict[str, object]] = []

    class _FakeResponse:
        def __init__(self, payload: bytes = b"") -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        captured_requests.append(
            {
                "url": request.full_url,
                "method": request.get_method(),
                "headers": dict(request.header_items()),
                "payload": (
                    None
                    if request.data is None
                    else (
                        request.data
                        if request.get_method() == "PUT"
                        else json.loads(request.data.decode("utf-8"))
                    )
                ),
            }
        )
        if request.full_url == "https://api.example.test/v1/source-snapshots":
            return _FakeResponse(
                json.dumps(
                    {
                        "source_snapshot_id": "src_123",
                        "upload_url": "https://uploads.example.test/source-bundle",
                        "upload_headers": {"x-goog-content-sha256": "UNSIGNED-PAYLOAD"},
                    }
                ).encode("utf-8")
            )
        if request.full_url.endswith("/finalize"):
            return _FakeResponse(
                json.dumps(
                    {
                        "source_snapshot_id": "src_123",
                        "status": "ready",
                    }
                ).encode("utf-8")
            )
        if request.full_url == "https://api.example.test/v1/agents":
            return _FakeResponse(
                json.dumps(
                    {
                        "agent_id": "agt_123",
                        "version_id": "ver_123",
                    }
                ).encode("utf-8")
            )
        return _FakeResponse()

    response = deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        environ={},
        opener=fake_opener,
    )

    assert response == {"agent_id": "agt_123", "version_id": "ver_123"}
    assert captured_requests[0]["url"] == "https://api.example.test/v1/source-snapshots"
    assert captured_requests[1]["method"] == "PUT"
    assert captured_requests[2]["url"].endswith("/v1/source-snapshots/src_123/finalize")
    assert captured_requests[3]["payload"]["manifest"]["custom_tools"][0]["name"] == (
        "repo_search"
    )
    assert captured_requests[3]["payload"]["manifest"]["built_in_tools"] == [
        {"execution_mode": "ephemeral", "name": "sandbox.exec"}
    ]


def test_deploy_checkout_cleans_up_reserved_snapshot_after_publish_failure(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    deleted: list[str] = []

    class _FakeResponse:
        def __init__(self, payload: bytes = b"") -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        if request.full_url == "https://api.example.test/v1/source-snapshots":
            return _FakeResponse(
                json.dumps(
                    {
                        "source_snapshot_id": "src_123",
                        "upload_url": "https://uploads.example.test/source-bundle",
                        "upload_headers": {},
                    }
                ).encode("utf-8")
            )
        if request.full_url.endswith("/finalize"):
            return _FakeResponse(
                json.dumps({"source_snapshot_id": "src_123", "status": "ready"}).encode(
                    "utf-8"
                )
            )
        if request.full_url.endswith("/v1/source-snapshots/src_123"):
            deleted.append(request.full_url)
            return _FakeResponse(b"{}")
        if request.full_url == "https://api.example.test/v1/agents":
            raise HTTPError(
                request.full_url,
                400,
                "Bad Request",
                hdrs=None,
                fp=io.BytesIO(b'{"detail":"publish rejected"}'),
            )
        return _FakeResponse()

    with pytest.raises(
        DariApiError,
        match='Dari API request failed for /v1/agents with 400: \\{"detail":"publish rejected"\\}',
    ):
        deploy_checkout(
            tmp_path,
            api_url="https://api.example.test",
            api_key="test-api-key",
            environ={},
            opener=fake_opener,
        )

    assert deleted == ["https://api.example.test/v1/source-snapshots/src_123"]


def test_main_deploy_dry_run_prints_full_prepared_payload(
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    write_valid_bundle(tmp_path)

    exit_code = main(["deploy", str(tmp_path), "--dry-run"])

    assert exit_code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["manifest"]["name"] == "support-agent"
    assert payload["steps"][3]["payload"]["manifest"]["harness"] == "openai-agents"

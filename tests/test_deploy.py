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
                "harness: pi",
                "instructions:",
                "  system: prompts/system.md",
                "runtime:",
                "  dockerfile: Dockerfile",
                "sandbox:",
                "  provider: e2b",
                "  provider_api_key_secret: E2B_API_KEY",
                "llm:",
                "  model: anthropic/claude-sonnet-4.6",
                "  base_url: https://openrouter.ai/api/v1",
                "  api_key_secret: OPENROUTER_API_KEY",
                "tools:",
                "  - name: repo_search",
                "    path: tools/repo_search",
                "    kind: main",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def write_pi_bundle(repo_root: Path) -> None:
    write_valid_bundle(repo_root)


def write_valid_skill(repo_root: Path) -> None:
    (repo_root / "skills" / "review").mkdir(parents=True, exist_ok=True)
    (repo_root / "skills" / "review" / "SKILL.md").write_text(
        "\n".join(
            [
                "---",
                "name: review",
                "description: Review code changes.",
                "---",
                "",
                "Review the current code changes.",
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
def test_build_source_bundle_respects_gitignore_for_git_checkouts(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / ".gitignore").write_text(".env\n.dari/\n", encoding="utf-8")
    (tmp_path / ".env").write_text("SECRET=1\n", encoding="utf-8")
    (tmp_path / ".dari").mkdir()
    (tmp_path / ".dari" / "deploy-state.json").write_text("{}", encoding="utf-8")
    _init_git_repo(tmp_path)

    bundle = build_source_bundle(tmp_path)

    with tarfile.open(fileobj=io.BytesIO(bundle.content), mode="r:gz") as archive:
        names = sorted(archive.getnames())

    assert names == [
        ".gitignore",
        "Dockerfile",
        "dari.yml",
        "prompts/system.md",
        "tools/repo_search/handler.py",
        "tools/repo_search/input.schema.json",
        "tools/repo_search/tool.yml",
    ]


def test_build_source_bundle_rejects_symlinks(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    (tmp_path / "linked.md").symlink_to(tmp_path / "prompts" / "system.md")

    with pytest.raises(ValueError, match="symlink"):
        build_source_bundle(tmp_path)


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


def test_collect_source_metadata_uses_ci_environment_fallbacks(tmp_path: Path) -> None:
    metadata = collect_source_metadata(
        tmp_path,
        environ={
            "CI": "true",
            "GITHUB_ACTIONS": "true",
            "GITHUB_SHA": "abc123",
            "GITHUB_REF": "refs/heads/main",
            "GITHUB_RUN_ID": "42",
            "GITHUB_SERVER_URL": "https://github.com",
            "GITHUB_REPOSITORY": "dari/agent-host",
        },
    )

    assert metadata == {
        "ci_provider": "github_actions",
        "ci_run_url": "https://github.com/dari/agent-host/actions/runs/42",
        "git_commit_sha": "abc123",
        "git_ref": "refs/heads/main",
        "github_run_id": "42",
        "origin": "ci",
    }


def test_prepare_deploy_flow_outputs_bundle_and_server_publish_steps(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)

    prepared = prepare_deploy_flow(
        tmp_path,
        environ={"CI": "true", "GITHUB_SHA": "deadbeef"},
    )
    dry_run = prepared.to_dict()

    assert "manifest" not in dry_run
    assert dry_run["bundle"]["metadata"] == {
        "git_commit_sha": "deadbeef",
        "origin": "ci",
    }
    assert dry_run["steps"][0]["endpoint"] == "/v1/source-snapshots"
    assert dry_run["steps"][2]["endpoint"] == (
        "/v1/source-snapshots/<source_snapshot_id from reserve step>/finalize"
    )
    assert dry_run["steps"][3]["endpoint"] == (
        "/v1/source-snapshots/<source_snapshot_id from reserve step>/manifest"
    )
    assert dry_run["steps"][3]["method"] == "GET"
    assert dry_run["steps"][4]["endpoint"] == "/v1/agents"
    assert dry_run["steps"][4]["payload"] == {
        "source_snapshot_id": "<source_snapshot_id from reserve step>",
    }


def test_deploy_checkout_reserves_uploads_finalizes_and_publishes(
    tmp_path: Path,
) -> None:
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
        if request.full_url.endswith("/v1/source-snapshots/src_123/manifest"):
            return _FakeResponse(
                json.dumps(
                    {
                        "source_snapshot_id": "src_123",
                        "manifest": {"name": "support-agent", "harness": "pi"},
                        "system_prompt": "...",
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
        environ={"CI": "true", "GITHUB_SHA": "abc123"},
        opener=fake_opener,
    )

    assert [request["method"] for request in captured_requests] == [
        "POST",
        "PUT",
        "POST",
        "GET",
        "POST",
    ]
    assert captured_requests[0]["url"] == "https://api.example.test/v1/source-snapshots"
    assert captured_requests[0]["payload"]["metadata"] == {
        "git_commit_sha": "abc123",
        "origin": "ci",
    }
    assert captured_requests[2]["url"].endswith("/v1/source-snapshots/src_123/finalize")
    assert captured_requests[3]["url"].endswith("/v1/source-snapshots/src_123/manifest")
    assert captured_requests[4]["payload"] == {"source_snapshot_id": "src_123"}
    assert response == {"agent_id": "agt_123", "version_id": "ver_123"}


def test_deploy_checkout_emits_progress_events_in_order(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)

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
                        "source_snapshot_id": "src_xyz",
                        "upload_url": "https://uploads.example.test/bundle",
                        "upload_headers": {},
                    }
                ).encode("utf-8")
            )
        if request.full_url.endswith("/finalize"):
            return _FakeResponse(
                json.dumps(
                    {"source_snapshot_id": "src_xyz", "status": "ready"}
                ).encode("utf-8")
            )
        if request.full_url.endswith("/manifest"):
            return _FakeResponse(b"{}")
        if request.full_url == "https://api.example.test/v1/agents":
            return _FakeResponse(
                json.dumps(
                    {"agent_id": "agt_xyz", "version_id": "ver_xyz"}
                ).encode("utf-8")
            )
        return _FakeResponse()

    events: list[tuple[str, dict[str, object]]] = []

    def progress(event: str, data) -> None:
        events.append((event, dict(data)))

    deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        environ={},
        opener=fake_opener,
        progress=progress,
    )

    assert [name for name, _ in events] == [
        "package:start",
        "package:complete",
        "reserve:start",
        "reserve:complete",
        "upload:start",
        "upload:complete",
        "finalize:start",
        "finalize:complete",
        "validate:start",
        "validate:complete",
        "publish:start",
        "publish:complete",
    ]

    by_name = dict(events)
    assert isinstance(by_name["package:complete"]["size_bytes"], int)
    assert by_name["package:complete"]["size_bytes"] > 0
    assert by_name["package:complete"]["file_count"] == 6
    assert by_name["reserve:complete"]["source_snapshot_id"] == "src_xyz"
    assert (
        by_name["upload:start"]["size_bytes"]
        == by_name["package:complete"]["size_bytes"]
    )
    assert by_name["publish:start"]["is_new_agent"] is True
    assert by_name["publish:complete"]["agent_id"] == "agt_xyz"


def test_deploy_checkout_persists_agent_id_for_later_publishes(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    captured_publish_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_1","upload_url":"https://uploads.example.test/1","upload_headers":{},"status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_1","status":"ready"}',
            b'{"source_snapshot_id":"src_1","manifest":{"name":"support-agent"},"system_prompt":"..."}',
            b'{"id":"agt_123","agent_id":"agt_123","active_version_id":"ver_1"}',
            b'{"source_snapshot_id":"src_2","upload_url":"https://uploads.example.test/2","upload_headers":{},"status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_2","status":"ready"}',
            b'{"source_snapshot_id":"src_2","manifest":{"name":"support-agent"},"system_prompt":"..."}',
            b'{"agent_id":"agt_123","version_id":"ver_2"}',
        ]
    )

    class _FakeResponse:
        def __init__(self, payload: bytes) -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        if request.full_url.startswith("https://api.example.test/v1/agents"):
            captured_publish_urls.append(request.full_url)
        return _FakeResponse(next(responses))

    deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        opener=fake_opener,
    )
    prepared = prepare_deploy_flow(
        tmp_path,
        api_url="https://api.example.test",
    )
    deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        opener=fake_opener,
    )

    assert captured_publish_urls == [
        "https://api.example.test/v1/agents",
        "https://api.example.test/v1/agents/agt_123/versions",
    ]
    assert prepared.publish_endpoint == "/v1/agents/agt_123/versions"
    state_path = tmp_path / ".dari" / "deploy-state.json"
    assert state_path.exists()
    assert json.loads(state_path.read_text(encoding="utf-8")) == {
        "agents": {"https://api.example.test": "agt_123"},
        "version": 1,
    }


def test_deploy_checkout_aborts_when_finalize_fails(tmp_path: Path) -> None:
    write_valid_bundle(tmp_path)
    requested_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_123","status":"failed","failure_reason":"Uploaded archive checksum did not match."}',
        ]
    )

    class _FakeResponse:
        def __init__(self, payload: bytes) -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        requested_urls.append(request.full_url)
        return _FakeResponse(next(responses))

    with pytest.raises(DariApiError, match="did not become ready"):
        deploy_checkout(
            tmp_path,
            api_url="https://api.example.test",
            api_key="test-api-key",
            opener=fake_opener,
        )

    assert requested_urls == [
        "https://api.example.test/v1/source-snapshots",
        "https://uploads.example.test/signed",
        "https://api.example.test/v1/source-snapshots/src_123/finalize",
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
        if request.full_url.endswith("/v1/source-snapshots/src_123/manifest"):
            return _FakeResponse(
                json.dumps(
                    {
                        "source_snapshot_id": "src_123",
                        "manifest": {"name": "support-agent"},
                        "system_prompt": "...",
                    }
                ).encode("utf-8")
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


def _http_error(url: str, *, code: int, body: bytes) -> HTTPError:
    return HTTPError(url, code, "error", hdrs=None, fp=io.BytesIO(body))


def test_deploy_checkout_deletes_ready_snapshot_when_publish_fails(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    requested_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_123","status":"ready"}',
            b'{"source_snapshot_id":"src_123","manifest":{"name":"support-agent"},"system_prompt":"..."}',
            _http_error(
                "https://api.example.test/v1/agents",
                code=403,
                body=b'{"detail":"forbidden"}',
            ),
            b"",
        ]
    )

    class _FakeResponse:
        def __init__(self, payload: bytes) -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        requested_urls.append(request.full_url)
        response = next(responses)
        if isinstance(response, HTTPError):
            raise response
        return _FakeResponse(response)

    with pytest.raises(DariApiError, match="failed for /v1/agents with 403"):
        deploy_checkout(
            tmp_path,
            api_url="https://api.example.test",
            api_key="test-api-key",
            opener=fake_opener,
        )

    assert requested_urls == [
        "https://api.example.test/v1/source-snapshots",
        "https://uploads.example.test/signed",
        "https://api.example.test/v1/source-snapshots/src_123/finalize",
        "https://api.example.test/v1/source-snapshots/src_123/manifest",
        "https://api.example.test/v1/agents",
        "https://api.example.test/v1/source-snapshots/src_123",
    ]


def test_deploy_checkout_surfaces_snapshot_id_when_cleanup_fails(
    tmp_path: Path,
) -> None:
    write_valid_bundle(tmp_path)
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_123","status":"ready"}',
            b'{"source_snapshot_id":"src_123","manifest":{"name":"support-agent"},"system_prompt":"..."}',
            _http_error(
                "https://api.example.test/v1/agents",
                code=500,
                body=b'{"detail":"boom"}',
            ),
            _http_error(
                "https://api.example.test/v1/source-snapshots/src_123",
                code=409,
                body=b'{"detail":"in use"}',
            ),
        ]
    )

    class _FakeResponse:
        def __init__(self, payload: bytes) -> None:
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb) -> None:
            return None

        def read(self) -> bytes:
            return self._payload

    def fake_opener(request):
        response = next(responses)
        if isinstance(response, HTTPError):
            raise response
        return _FakeResponse(response)

    with pytest.raises(
        DariApiError,
        match="source snapshot src_123: .*cleanup error: Dari API request failed for /v1/source-snapshots/src_123 with 409",
    ):
        deploy_checkout(
            tmp_path,
            api_url="https://api.example.test",
            api_key="test-api-key",
            opener=fake_opener,
        )


def test_main_deploy_dry_run_prints_full_prepared_payload(
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    write_valid_bundle(tmp_path)

    exit_code = main(["deploy", str(tmp_path), "--dry-run"])

    assert exit_code == 0
    payload = json.loads(capsys.readouterr().out)
    assert "manifest" not in payload
    assert payload["steps"][0]["endpoint"] == "/v1/source-snapshots"
    assert payload["steps"][4]["endpoint"] == "/v1/agents"
    assert payload["steps"][4]["payload"] == {
        "source_snapshot_id": "<source_snapshot_id from reserve step>",
    }

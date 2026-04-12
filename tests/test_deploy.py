from __future__ import annotations

import io
import json
import shutil
import subprocess
import tarfile
from urllib.error import HTTPError

import pytest

from dari_cli.__main__ import main
from dari_cli.deploy import (
    DariApiError,
    DeployConfigurationError,
    build_source_bundle,
    collect_source_metadata,
    deploy_checkout,
    prepare_deploy_flow,
)


def _git_available() -> bool:
    return shutil.which("git") is not None


def _init_git_repo(tmp_path) -> str:
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


def test_build_source_bundle_packages_checkout_and_skips_git_metadata(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    (tmp_path / ".git").mkdir()
    (tmp_path / ".git" / "config").write_text("[core]\n", encoding="utf-8")
    (tmp_path / ".venv").mkdir()
    (tmp_path / ".venv" / "ignore.txt").write_text("ignored\n", encoding="utf-8")

    bundle = build_source_bundle(tmp_path)

    assert bundle.file_count == 2
    assert bundle.size_bytes > 0
    assert bundle.sha256

    with tarfile.open(fileobj=io.BytesIO(bundle.content), mode="r:gz") as archive:
        names = sorted(archive.getnames())

    assert names == ["agent.py", "dari.yml"]


@pytest.mark.skipif(not _git_available(), reason="git is required for this test")
def test_build_source_bundle_respects_gitignore_for_git_checkouts(tmp_path) -> None:
    (tmp_path / ".gitignore").write_text(".env\n.dari/\n", encoding="utf-8")
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    (tmp_path / ".env").write_text("SECRET=1\n", encoding="utf-8")
    (tmp_path / ".dari").mkdir()
    (tmp_path / ".dari" / "deploy-state.json").write_text("{}", encoding="utf-8")
    _init_git_repo(tmp_path)

    bundle = build_source_bundle(tmp_path)

    with tarfile.open(fileobj=io.BytesIO(bundle.content), mode="r:gz") as archive:
        names = sorted(archive.getnames())

    assert names == [".gitignore", "agent.py", "dari.yml"]


def test_build_source_bundle_rejects_symlinks(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    (tmp_path / "linked.py").symlink_to(tmp_path / "agent.py")

    with pytest.raises(ValueError, match="symlink"):
        build_source_bundle(tmp_path)


@pytest.mark.skipif(not _git_available(), reason="git is required for this test")
def test_collect_source_metadata_includes_git_provenance(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    commit_sha = _init_git_repo(tmp_path)
    (tmp_path / "agent.py").write_text("agent = 'dirty'\n", encoding="utf-8")

    metadata = collect_source_metadata(tmp_path, environ={})

    assert metadata == {
        "git_commit_sha": commit_sha,
        "git_dirty": True,
        "git_ref": "main",
        "origin": "local_cli",
    }


def test_collect_source_metadata_uses_ci_environment_fallbacks(tmp_path) -> None:
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


def test_prepare_deploy_flow_includes_snapshot_reserve_and_publish_steps(
    tmp_path,
) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: opencode",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )

    prepared = prepare_deploy_flow(
        tmp_path,
        environ={"CI": "true", "GITHUB_SHA": "deadbeef"},
    )
    dry_run = prepared.to_dict()

    assert dry_run["steps"][0]["endpoint"] == "/v1/source-snapshots"
    assert dry_run["steps"][0]["payload"]["metadata"] == {
        "git_commit_sha": "deadbeef",
        "origin": "ci",
    }
    assert dry_run["steps"][2]["endpoint"] == (
        "/v1/source-snapshots/<source_snapshot_id from reserve step>/finalize"
    )
    assert dry_run["steps"][3]["endpoint"] == "/v1/agents"
    assert (
        dry_run["steps"][3]["payload"]["source_snapshot_id"]
        == "<source_snapshot_id from reserve step>"
    )
    assert dry_run["steps"][3]["payload"]["manifest"]["sdk"] == "opencode"


def test_prepare_deploy_flow_includes_execution_backend_id_for_pi_sdk(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: pi",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )

    prepared = prepare_deploy_flow(
        tmp_path,
        execution_backend_id=" execb_123 ",
    )

    assert prepared.execution_backend_id == "execb_123"
    assert prepared.to_dict()["steps"][3]["payload"]["execution_backend_id"] == (
        "execb_123"
    )


def test_prepare_deploy_flow_requires_execution_backend_id_for_pi_sdk(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: pi",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )

    with pytest.raises(
        DeployConfigurationError,
        match="execution_backend_id is required for sdk 'pi'",
    ):
        prepare_deploy_flow(tmp_path)


def test_prepare_deploy_flow_rejects_execution_backend_id_for_non_pi_sdk(
    tmp_path,
) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")

    with pytest.raises(
        DeployConfigurationError,
        match="execution_backend_id is only supported for sdk 'pi'",
    ):
        prepare_deploy_flow(
            tmp_path,
            execution_backend_id="execb_123",
        )


def test_deploy_checkout_reserves_uploads_finalizes_and_publishes(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
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
                b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{"Content-Type":"application/gzip"},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}'
            )
        if request.full_url == "https://uploads.example.test/signed":
            return _FakeResponse()
        if (
            request.full_url
            == "https://api.example.test/v1/source-snapshots/src_123/finalize"
        ):
            return _FakeResponse(b'{"source_snapshot_id":"src_123","status":"ready"}')
        if request.full_url == "https://api.example.test/v1/agents":
            return _FakeResponse(b'{"id":"agt_123","active_version_id":"ver_1"}')
        raise AssertionError(f"Unexpected request URL: {request.full_url}")

    response = deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        environ={"CI": "true", "GITHUB_SHA": "abc123"},
        opener=fake_opener,
    )

    assert [request["url"] for request in captured_requests] == [
        "https://api.example.test/v1/source-snapshots",
        "https://uploads.example.test/signed",
        "https://api.example.test/v1/source-snapshots/src_123/finalize",
        "https://api.example.test/v1/agents",
    ]
    assert [request["method"] for request in captured_requests] == [
        "POST",
        "PUT",
        "POST",
        "POST",
    ]
    reserve_payload = captured_requests[0]["payload"]
    assert reserve_payload["format"] == "tar.gz"
    assert reserve_payload["metadata"] == {
        "git_commit_sha": "abc123",
        "origin": "ci",
    }
    upload_payload = captured_requests[1]["payload"]
    assert isinstance(upload_payload, bytes)
    assert upload_payload
    assert captured_requests[1]["headers"]["Content-type"] == "application/gzip"
    publish_payload = captured_requests[3]["payload"]
    assert publish_payload["name"] == "support-agent"
    assert publish_payload["manifest"]["entrypoint"] == "agent.py:agent"
    assert publish_payload["source_snapshot_id"] == "src_123"
    assert captured_requests[3]["headers"]["Authorization"] == "Bearer test-api-key"
    assert response == {"id": "agt_123", "active_version_id": "ver_1"}


def test_deploy_checkout_sends_execution_backend_id_for_pi_sdk(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: pi",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )
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
                b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{"Content-Type":"application/gzip"},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}'
            )
        if request.full_url == "https://uploads.example.test/signed":
            return _FakeResponse()
        if (
            request.full_url
            == "https://api.example.test/v1/source-snapshots/src_123/finalize"
        ):
            return _FakeResponse(b'{"source_snapshot_id":"src_123","status":"ready"}')
        if request.full_url == "https://api.example.test/v1/agents":
            return _FakeResponse(b'{"id":"agt_123","active_version_id":"ver_1"}')
        raise AssertionError(f"Unexpected request URL: {request.full_url}")

    deploy_checkout(
        tmp_path,
        api_url="https://api.example.test",
        api_key="test-api-key",
        execution_backend_id="execb_123",
        opener=fake_opener,
    )

    publish_payload = captured_requests[3]["payload"]
    assert publish_payload["manifest"]["sdk"] == "pi"
    assert publish_payload["execution_backend_id"] == "execb_123"


def test_deploy_checkout_persists_agent_id_for_later_publishes(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    captured_publish_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_1","upload_url":"https://uploads.example.test/1","upload_headers":{},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_1","status":"ready"}',
            b'{"id":"agt_123","agent_id":"agt_123","active_version_id":"ver_1"}',
            b'{"source_snapshot_id":"src_2","upload_url":"https://uploads.example.test/2","upload_headers":{},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_2","status":"ready"}',
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


def test_deploy_checkout_aborts_when_finalize_fails(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    requested_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}',
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


def _http_error(url: str, *, code: int, body: bytes) -> HTTPError:
    return HTTPError(url, code, "error", hdrs=None, fp=io.BytesIO(body))


def test_deploy_checkout_deletes_ready_snapshot_when_publish_fails(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    requested_urls: list[str] = []
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_123","status":"ready"}',
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
        "https://api.example.test/v1/agents",
        "https://api.example.test/v1/source-snapshots/src_123",
    ]


def test_deploy_checkout_surfaces_snapshot_id_when_cleanup_fails(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")
    responses = iter(
        [
            b'{"source_snapshot_id":"src_123","upload_url":"https://uploads.example.test/signed","upload_headers":{},"expires_at":"2026-04-04T00:00:00+00:00","status":"pending_upload"}',
            b"",
            b'{"source_snapshot_id":"src_123","status":"ready"}',
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


def test_main_supports_dry_run_output(tmp_path, capsys) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: openai-agents",
                "entrypoint: agent.py:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "agent.py").write_text("agent = object()\n", encoding="utf-8")

    exit_code = main(["deploy", str(tmp_path), "--dry-run"])
    output = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert output["steps"][0]["endpoint"] == "/v1/source-snapshots"
    assert output["steps"][3]["endpoint"] == "/v1/agents"


def test_main_supports_execution_backend_id_in_dry_run_output(
    tmp_path,
    capsys,
) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: pi",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )

    exit_code = main(
        [
            "deploy",
            str(tmp_path),
            "--dry-run",
            "--execution-backend-id",
            "execb_123",
        ]
    )
    output = json.loads(capsys.readouterr().out)

    assert exit_code == 0
    assert output["steps"][3]["payload"]["execution_backend_id"] == "execb_123"


def test_main_requires_execution_backend_id_for_pi_sdk(tmp_path) -> None:
    (tmp_path / "dari.yml").write_text(
        "\n".join(
            [
                "name: support-agent",
                "sdk: pi",
                "entrypoint: src/agent.ts:agent",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "agent.ts").write_text(
        "export const agent = {};\n",
        encoding="utf-8",
    )

    with pytest.raises(
        SystemExit,
        match="execution_backend_id is required for sdk 'pi'",
    ):
        main(["deploy", str(tmp_path), "--dry-run"])

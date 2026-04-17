from __future__ import annotations

from contextlib import contextmanager
import io
import json
from types import SimpleNamespace

import dari_cli.management as management
from dari_cli.__main__ import main
from dari_cli.management import (
    DariCliAuthError,
    DariCliNetworkError,
    login,
)
from dari_cli.state import (
    CliState,
    StoredOrganization,
    StoredSupabaseSession,
    load_cli_state,
    save_cli_state,
)
from supabase_auth.errors import AuthRetryableError
import pytest


def _build_cli_state(
    *,
    api_url: str = "https://api.example.test",
    expires_at: int | None = 4102444800,
    current_org_id: str | None = "org_123",
) -> CliState:
    organizations: dict[str, StoredOrganization] = {}
    if current_org_id is not None:
        organizations[current_org_id] = StoredOrganization(
            id=current_org_id,
            name="Team Blue",
            slug="team-blue",
            role="owner",
            api_key="dari_blue_key",
        )
    return CliState(
        api_url=api_url,
        supabase_session=StoredSupabaseSession(
            access_token="access-token",
            refresh_token="refresh-token",
            expires_at=expires_at,
            user_id="sup_user_123",
            email="user@example.test",
            display_name="User Example",
        ),
        current_org_id=current_org_id,
        organizations=organizations,
    )


def test_login_opens_browser_and_persists_state(monkeypatch, tmp_path) -> None:
    config_dir = tmp_path / "config"
    browser_urls: list[str] = []

    class _FakeAuth:
        def __init__(self) -> None:
            self.oauth_credentials = None
            self.exchange_params = None

        def sign_in_with_oauth(self, credentials):  # noqa: ANN001
            self.oauth_credentials = credentials
            return SimpleNamespace(url="https://supabase.example.test/oauth")

        def exchange_code_for_session(self, params):  # noqa: ANN001
            self.exchange_params = params
            return SimpleNamespace(
                session=SimpleNamespace(
                    access_token="access-token",
                    refresh_token="refresh-token",
                    expires_at=1234567890,
                    user=SimpleNamespace(
                        id="sup_user_123",
                        email="user@example.test",
                        user_metadata={"full_name": "User Example"},
                    ),
                )
            )

    fake_auth = _FakeAuth()

    @contextmanager
    def fake_callback_server():
        yield SimpleNamespace(
            redirect_url="http://127.0.0.1:9999/callback",
            state="supabase-managed-state",
            code="auth-code",
            error=None,
            wait=lambda timeout_seconds: SimpleNamespace(  # noqa: ARG005
                redirect_url="http://127.0.0.1:9999/callback",
                state="supabase-managed-state",
                code="auth-code",
                error=None,
            ),
        )

    def fake_bootstrap_and_select_current_org(state, **kwargs):  # noqa: ANN001, ANN003
        state.current_org_id = "org_123"
        state.organizations["org_123"] = StoredOrganization(
            id="org_123",
            name="User Example Personal",
            slug="user-example-personal",
            role="owner",
            api_key="dari_managed_key",
        )
        return state

    monkeypatch.setattr(
        "dari_cli.management._fetch_auth_config",
        lambda api_url, opener: {  # noqa: ARG005
            "supabase_url": "https://supabase.example.test",
            "supabase_publishable_key": "sb_test_key",
        },
    )
    monkeypatch.setattr(
        "dari_cli.management._build_supabase_client",
        lambda auth_config: SimpleNamespace(auth=fake_auth),  # noqa: ARG005
    )
    monkeypatch.setattr(
        "dari_cli.management._run_local_callback_server",
        fake_callback_server,
    )
    monkeypatch.setattr(
        "dari_cli.management._bootstrap_and_select_current_org",
        fake_bootstrap_and_select_current_org,
    )

    state = login(
        api_url="https://api.example.test/",
        environ={"DARI_CONFIG_DIR": str(config_dir)},
        browser_opener=lambda url: browser_urls.append(url) or True,
    )

    assert browser_urls == ["https://supabase.example.test/oauth"]
    assert fake_auth.oauth_credentials["provider"] == "google"
    assert fake_auth.oauth_credentials["options"] == {
        "redirect_to": "http://127.0.0.1:9999/callback"
    }
    assert fake_auth.exchange_params["auth_code"] == "auth-code"
    assert state.current_org_id == "org_123"
    assert state.organizations["org_123"].api_key == "dari_managed_key"

    saved = json.loads((config_dir / "state.json").read_text(encoding="utf-8"))
    assert saved["api_url"] == "https://api.example.test"
    assert saved["current_org_id"] == "org_123"
    assert saved["supabase_session"]["email"] == "user@example.test"


def test_login_reports_manual_url_when_browser_open_fails(
    monkeypatch, tmp_path
) -> None:
    config_dir = tmp_path / "config"

    class _FakeAuth:
        def sign_in_with_oauth(self, credentials):  # noqa: ANN001
            return SimpleNamespace(url="https://supabase.example.test/oauth")

    @contextmanager
    def fake_callback_server():
        yield SimpleNamespace(
            redirect_url="http://127.0.0.1:9999/callback",
            wait=lambda timeout_seconds: None,  # noqa: ARG005
        )

    monkeypatch.setattr(
        "dari_cli.management._fetch_auth_config",
        lambda api_url, opener: {  # noqa: ARG005
            "supabase_url": "https://supabase.example.test",
            "supabase_publishable_key": "sb_test_key",
        },
    )
    monkeypatch.setattr(
        "dari_cli.management._build_supabase_client",
        lambda auth_config: SimpleNamespace(auth=_FakeAuth()),  # noqa: ARG005
    )
    monkeypatch.setattr(
        "dari_cli.management._run_local_callback_server",
        fake_callback_server,
    )

    with pytest.raises(
        DariCliAuthError,
        match=r"Open this URL manually: https://supabase\.example\.test/oauth",
    ):
        login(
            api_url="https://api.example.test",
            environ={"DARI_CONFIG_DIR": str(config_dir)},
            browser_opener=lambda url: False,  # noqa: ARG005
        )


def test_deploy_uses_cached_current_org_api_key(monkeypatch, tmp_path) -> None:
    config_dir = tmp_path / "config"
    state = CliState(
        api_url="https://api.example.test/",
        supabase_session=StoredSupabaseSession(
            access_token="access-token",
            refresh_token="refresh-token",
            expires_at=None,
            user_id="sup_user_123",
            email="user@example.test",
            display_name="User Example",
        ),
        current_org_id="org_123",
        organizations={
            "org_123": StoredOrganization(
                id="org_123",
                name="Support",
                slug="support",
                role="owner",
                api_key="dari_cached_key",
            )
        },
    )
    save_cli_state(state, environ={"DARI_CONFIG_DIR": str(config_dir)})
    monkeypatch.setenv("DARI_CONFIG_DIR", str(config_dir))

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
                "  - name: repo_search",
                "    path: tools/repo_search",
                "    kind: main",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "Dockerfile").write_text(
        "FROM node:20-bookworm\nWORKDIR /bundle\nCOPY . /bundle\n",
        encoding="utf-8",
    )
    (tmp_path / "prompts").mkdir()
    (tmp_path / "prompts" / "system.md").write_text(
        "You are a cached-org deploy test bundle.\n",
        encoding="utf-8",
    )
    (tmp_path / "tools" / "repo_search").mkdir(parents=True)
    (tmp_path / "tools" / "repo_search" / "tool.yml").write_text(
        "\n".join(
            [
                "name: repo_search",
                "description: Search the repository for matching content.",
                "input_schema: input.schema.json",
                "runtime: typescript",
                "handler: handler.ts:main",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    (tmp_path / "tools" / "repo_search" / "input.schema.json").write_text(
        '{"type":"object","properties":{"query":{"type":"string"}}}\n',
        encoding="utf-8",
    )
    (tmp_path / "tools" / "repo_search" / "handler.ts").write_text(
        "export async function main() { return { matches: [] }; }\n",
        encoding="utf-8",
    )

    captured: dict[str, str] = {}

    def fake_deploy_checkout(*args, **kwargs):  # noqa: ANN002, ANN003
        captured["api_key"] = kwargs["api_key"]
        return {"id": "agt_123"}

    monkeypatch.setattr("dari_cli.__main__.deploy_checkout", fake_deploy_checkout)

    exit_code = main(
        [
            "deploy",
            str(tmp_path),
            "--api-url",
            "https://api.example.test",
        ]
    )

    assert exit_code == 0
    assert captured["api_key"] == "dari_cached_key"


def test_org_switch_command_reports_new_current_org(monkeypatch, capsys) -> None:
    returned_state = CliState(
        api_url="https://api.example.test",
        current_org_id="org_456",
        organizations={
            "org_456": StoredOrganization(
                id="org_456",
                name="Team Blue",
                slug="team-blue",
                role="admin",
                api_key="dari_blue_key",
            )
        },
    )
    monkeypatch.setattr(
        "dari_cli.__main__.switch_organization",
        lambda **kwargs: returned_state,  # noqa: ARG005
    )

    exit_code = main(
        [
            "org",
            "switch",
            "team-blue",
            "--api-url",
            "https://api.example.test",
        ]
    )

    assert exit_code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["current_org_id"] == "org_456"
    assert payload["organization"]["slug"] == "team-blue"


def test_credentials_add_command_prompts_for_value(monkeypatch, capsys) -> None:
    monkeypatch.setattr(
        "dari_cli.__main__.getpass.getpass",
        lambda prompt: "sk-hidden" if prompt == "OPENAI_API_KEY: " else pytest.fail(),
    )
    monkeypatch.setattr(
        "dari_cli.__main__.upsert_credential",
        lambda **kwargs: {  # noqa: ARG005
            "id": "cred_123",
            "name": "OPENAI_API_KEY",
            "created": True,
        },
    )

    exit_code = main(
        [
            "credentials",
            "add",
            "OPENAI_API_KEY",
            "--api-url",
            "https://api.example.test",
        ]
    )

    assert exit_code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["name"] == "OPENAI_API_KEY"
    assert payload["created"] is True


def test_credentials_add_command_reads_value_from_stdin(monkeypatch, capsys) -> None:
    monkeypatch.setattr("sys.stdin", io.StringIO("sk-from-stdin\n"))
    monkeypatch.setattr(
        "dari_cli.__main__.upsert_credential",
        lambda **kwargs: {  # noqa: ARG005
            "id": "cred_123",
            "name": kwargs["name"],
            "created": False,
        },
    )

    exit_code = main(
        [
            "credentials",
            "add",
            "OPENAI_API_KEY",
            "--value-stdin",
            "--api-url",
            "https://api.example.test",
        ]
    )

    assert exit_code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["name"] == "OPENAI_API_KEY"
    assert payload["created"] is False


def test_credentials_add_command_warns_for_positional_value(
    monkeypatch, capsys
) -> None:
    captured: dict[str, str] = {}

    def fake_upsert_credential(**kwargs):  # noqa: ANN003
        captured["value"] = kwargs["value"]
        return {"id": "cred_123", "name": kwargs["name"], "created": True}

    monkeypatch.setattr(
        "dari_cli.__main__.upsert_credential",
        fake_upsert_credential,
    )

    exit_code = main(
        [
            "credentials",
            "add",
            "OPENAI_API_KEY",
            "sk-inline",
            "--api-url",
            "https://api.example.test",
        ]
    )

    assert exit_code == 0
    assert captured["value"] == "sk-inline"
    assert "shell history and process arguments" in capsys.readouterr().err


def test_credentials_list_without_login_prints_clean_error(
    monkeypatch, capsys, tmp_path
) -> None:
    monkeypatch.setenv("DARI_CONFIG_DIR", str(tmp_path / "config"))

    exit_code = main(
        [
            "credentials",
            "list",
            "--api-url",
            "https://api.example.test",
        ]
    )

    captured = capsys.readouterr()
    assert exit_code == 1
    assert captured.out == ""
    assert (
        captured.err.strip()
        == "No CLI login is available for this API URL. Run `dari auth login` first."
    )
    assert "Traceback" not in captured.err


def test_credentials_list_without_current_org_prints_clean_error(
    monkeypatch, capsys, tmp_path
) -> None:
    config_dir = tmp_path / "config"
    save_cli_state(
        _build_cli_state(current_org_id=None),
        environ={"DARI_CONFIG_DIR": str(config_dir)},
    )
    monkeypatch.setenv("DARI_CONFIG_DIR", str(config_dir))

    exit_code = main(
        [
            "credentials",
            "list",
            "--api-url",
            "https://api.example.test",
        ]
    )

    captured = capsys.readouterr()
    assert exit_code == 1
    assert captured.out == ""
    assert (
        captured.err.strip()
        == "No current organization is selected. Run `dari org switch <org>`."
    )
    assert "Traceback" not in captured.err


def test_main_prints_network_errors_without_traceback(monkeypatch, capsys) -> None:
    def fake_list_credentials(**kwargs):  # noqa: ANN003
        raise DariCliNetworkError("Agent Host management request failed. Try again.")

    monkeypatch.setattr("dari_cli.__main__.list_credentials", fake_list_credentials)

    exit_code = main(
        [
            "credentials",
            "list",
            "--api-url",
            "https://api.example.test",
        ]
    )

    captured = capsys.readouterr()
    assert exit_code == 1
    assert captured.out == ""
    assert captured.err.strip() == "Agent Host management request failed. Try again."
    assert "Traceback" not in captured.err


def test_load_authenticated_state_skips_refresh_for_fresh_session(
    monkeypatch, tmp_path
) -> None:
    config_dir = tmp_path / "config"
    save_cli_state(
        _build_cli_state(),
        environ={"DARI_CONFIG_DIR": str(config_dir)},
    )

    def fail_refresh(*args, **kwargs):  # noqa: ANN002, ANN003
        pytest.fail("fresh sessions should not trigger a Supabase refresh")

    monkeypatch.setattr(management, "_refresh_supabase_session", fail_refresh)

    authenticated_state = management._load_authenticated_state(
        api_url="https://api.example.test",
        environ={"DARI_CONFIG_DIR": str(config_dir)},
        opener=lambda *args, **kwargs: pytest.fail("no request should be issued"),  # noqa: ARG005
    )

    assert authenticated_state.used_cached_access_token is True
    assert (
        authenticated_state.state.supabase_session is not None
        and authenticated_state.state.supabase_session.access_token == "access-token"
    )


def test_load_authenticated_state_refreshes_session_without_expiry(
    monkeypatch, tmp_path
) -> None:
    config_dir = tmp_path / "config"
    save_cli_state(
        _build_cli_state(expires_at=None),
        environ={"DARI_CONFIG_DIR": str(config_dir)},
    )

    def fake_refresh(state, **kwargs):  # noqa: ANN001, ANN003
        state.supabase_session = StoredSupabaseSession(
            access_token="refreshed-access-token",
            refresh_token="refreshed-refresh-token",
            expires_at=4102444800,
            user_id="sup_user_123",
            email="user@example.test",
            display_name="User Example",
        )
        return state

    monkeypatch.setattr(management, "_refresh_supabase_session", fake_refresh)

    authenticated_state = management._load_authenticated_state(
        api_url="https://api.example.test",
        environ={"DARI_CONFIG_DIR": str(config_dir)},
        opener=lambda *args, **kwargs: None,
    )

    assert authenticated_state.used_cached_access_token is False
    persisted_state = load_cli_state(environ={"DARI_CONFIG_DIR": str(config_dir)})
    assert (
        persisted_state.supabase_session is not None
        and persisted_state.supabase_session.access_token == "refreshed-access-token"
    )


def test_list_credentials_retries_once_after_auth_rejection(
    monkeypatch, tmp_path
) -> None:
    config_dir = tmp_path / "config"
    save_cli_state(
        _build_cli_state(),
        environ={"DARI_CONFIG_DIR": str(config_dir)},
    )
    tokens_seen: list[str] = []

    def fake_management_request(**kwargs):  # noqa: ANN003
        tokens_seen.append(kwargs["bearer_token"])
        if len(tokens_seen) == 1:
            raise management._JsonRequestHttpError(401, "Unauthorized")
        return {"credentials": [{"name": "OPENAI_API_KEY"}]}

    def fake_refresh(state, **kwargs):  # noqa: ANN001, ANN003
        state.supabase_session = StoredSupabaseSession(
            access_token="refreshed-access-token",
            refresh_token="refreshed-refresh-token",
            expires_at=4102444800,
            user_id="sup_user_123",
            email="user@example.test",
            display_name="User Example",
        )
        return state

    monkeypatch.setattr(management, "_management_request", fake_management_request)
    monkeypatch.setattr(management, "_refresh_supabase_session", fake_refresh)

    credentials = management.list_credentials(
        api_url="https://api.example.test",
        environ={"DARI_CONFIG_DIR": str(config_dir)},
        opener=lambda *args, **kwargs: None,
    )

    assert credentials == [{"name": "OPENAI_API_KEY"}]
    assert tokens_seen == ["access-token", "refreshed-access-token"]
    persisted_state = load_cli_state(environ={"DARI_CONFIG_DIR": str(config_dir)})
    assert (
        persisted_state.supabase_session is not None
        and persisted_state.supabase_session.access_token == "refreshed-access-token"
    )


def test_management_request_timeout_raises_network_error() -> None:
    def fake_opener(request, timeout=None):  # noqa: ANN001
        raise TimeoutError("timed out")

    with pytest.raises(
        DariCliNetworkError,
        match="Agent Host management request failed\\. Try again\\.",
    ):
        management._management_request(
            api_url="https://api.example.test",
            path="/v1/organizations/org_123/credentials",
            bearer_token="access-token",
            opener=fake_opener,
        )


def test_refresh_supabase_session_retryable_error_is_network_error(
    monkeypatch,
) -> None:
    class _FakeAuth:
        def set_session(self, access_token, refresh_token):  # noqa: ANN001
            raise AuthRetryableError("temporary failure", 0)

    monkeypatch.setattr(
        management,
        "_fetch_auth_config",
        lambda api_url, opener: {  # noqa: ARG005
            "supabase_url": "https://supabase.example.test",
            "supabase_publishable_key": "sb_test_key",
        },
    )
    monkeypatch.setattr(
        management,
        "_build_supabase_client",
        lambda auth_config: SimpleNamespace(auth=_FakeAuth()),  # noqa: ARG005
    )

    with pytest.raises(
        DariCliNetworkError,
        match="Supabase session refresh failed\\. Try again\\.",
    ):
        management._refresh_supabase_session(
            _build_cli_state(),
            api_url="https://api.example.test",
            opener=lambda *args, **kwargs: None,
        )

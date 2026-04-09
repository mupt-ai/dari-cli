from __future__ import annotations

from contextlib import contextmanager
import json
from types import SimpleNamespace

from dari_cli.__main__ import main
from dari_cli.management import DariCliAuthError, login
from dari_cli.state import (
    CliState,
    StoredOrganization,
    StoredSupabaseSession,
    save_cli_state,
)
import pytest


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


def test_login_reports_manual_url_when_browser_open_fails(monkeypatch, tmp_path) -> None:
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

"""Browser auth and organization-management helpers for the Dari CLI."""

from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import socket
import threading
from typing import Any, Callable, Iterator
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlparse
from urllib.request import Request, urlopen
import webbrowser

from supabase import ClientOptions, create_client
from supabase_auth.types import CodeExchangeParams, SignInWithOAuthCredentials

from .state import (
    CliState,
    StoredOrganization,
    StoredSupabaseSession,
    load_cli_state,
    save_cli_state,
)

DEFAULT_API_URL = "https://api.dari.dev"
LOGIN_TIMEOUT_SECONDS = 300.0


class DariCliAuthError(RuntimeError):
    """Raised when CLI auth or org management fails."""


@dataclass(frozen=True, slots=True)
class AuthStatus:
    """Current CLI auth status."""

    api_url: str | None
    email: str | None
    current_org: StoredOrganization | None


def login(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
    browser_opener: Callable[[str], bool] = webbrowser.open,
    timeout_seconds: float = LOGIN_TIMEOUT_SECONDS,
) -> CliState:
    """Run browser login, bootstrap the user, and cache the managed org key."""
    api_url = _normalize_api_url(api_url)
    auth_config = _fetch_auth_config(api_url, opener=opener)
    supabase = _build_supabase_client(auth_config)

    with _run_local_callback_server() as callback:
        oauth_response = supabase.auth.sign_in_with_oauth(
            SignInWithOAuthCredentials(
                provider="google",
                options={
                    "redirect_to": callback.redirect_url,
                },
            )
        )
        try:
            browser_opened = browser_opener(oauth_response.url)
        except Exception as exc:
            raise DariCliAuthError(
                "Unable to open a browser for sign-in. "
                f"Open this URL manually: {oauth_response.url}"
            ) from exc
        if browser_opened is False:
            raise DariCliAuthError(
                "Unable to open a browser for sign-in. "
                f"Open this URL manually: {oauth_response.url}"
            )
        callback_result = callback.wait(timeout_seconds=timeout_seconds)

    if callback_result.error is not None:
        raise DariCliAuthError(callback_result.error)
    if callback_result.code is None:
        raise DariCliAuthError("Timed out waiting for browser login to complete.")

    auth_response = supabase.auth.exchange_code_for_session(
        CodeExchangeParams(
            auth_code=callback_result.code,
            redirect_to=callback.redirect_url,
        )
    )
    session = auth_response.session
    if session is None or session.user is None or session.user.email is None:
        raise DariCliAuthError("Supabase login did not return a usable session.")

    state = CliState(
        api_url=api_url,
        supabase_session=_stored_session_from_supabase_session(session),
    )
    state = _bootstrap_and_select_current_org(
        state,
        api_url=api_url,
        opener=opener,
        preferred_org_id=None,
    )
    save_cli_state(state, environ=environ)
    return state


def logout(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> None:
    """Clear local CLI auth state and best-effort revoke the Supabase session."""
    api_url = _normalize_api_url(api_url)
    state = load_cli_state(environ=environ)
    if not _api_urls_match(state.api_url, api_url) or state.supabase_session is None:
        return

    try:
        auth_config = _fetch_auth_config(api_url, opener=opener)
        supabase = _build_supabase_client(auth_config)
        supabase.auth.set_session(
            state.supabase_session.access_token,
            state.supabase_session.refresh_token,
        )
        supabase.auth.sign_out()
    except Exception:
        pass

    save_cli_state(CliState(), environ=environ)


def get_auth_status(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
) -> AuthStatus:
    """Return the local CLI auth status without making network calls."""
    api_url = _normalize_api_url(api_url)
    state = load_cli_state(environ=environ)
    if not _api_urls_match(state.api_url, api_url) or state.supabase_session is None:
        return AuthStatus(api_url=None, email=None, current_org=None)
    return AuthStatus(
        api_url=state.api_url,
        email=state.supabase_session.email,
        current_org=_current_org(state),
    )


def list_organizations(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> tuple[CliState, list[dict[str, Any]]]:
    """List organizations for the current user and sync local state."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    payload = _management_request(
        api_url=api_url,
        path="/v1/organizations",
        bearer_token=state.supabase_session.access_token,
        opener=opener,
    )
    organizations = list(payload["organizations"])
    state = _sync_organizations(state, organizations)
    save_cli_state(state, environ=environ)
    return state, organizations


def create_organization(
    *,
    api_url: str,
    name: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> CliState:
    """Create a new organization, switch to it, and cache its managed key."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    created = _management_request(
        api_url=api_url,
        path="/v1/organizations",
        bearer_token=state.supabase_session.access_token,
        payload={"name": name},
        opener=opener,
    )
    state = _upsert_organization(state, created)
    state = _ensure_current_org_key(
        state,
        api_url=api_url,
        organization_id=created["id"],
        opener=opener,
    )
    save_cli_state(state, environ=environ)
    return state


def switch_organization(
    *,
    api_url: str,
    identifier: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> CliState:
    """Switch the current org and cache a managed key for it."""
    state, organizations = list_organizations(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _match_organization(organizations, identifier)
    if organization is None:
        raise DariCliAuthError(f"Unknown organization {identifier!r}.")
    state = _ensure_current_org_key(
        state,
        api_url=api_url,
        organization_id=organization["id"],
        opener=opener,
    )
    save_cli_state(state, environ=environ)
    return state


def list_api_keys(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> list[dict[str, Any]]:
    """List API keys for the current org."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    organization = _require_current_org(state)
    payload = _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization.id}/api-keys",
        bearer_token=state.supabase_session.access_token,
        opener=opener,
    )
    return list(payload["api_keys"])


def create_api_key(
    *,
    api_url: str,
    label: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Create a manual org API key for the current org."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    organization = _require_current_org(state)
    return _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization.id}/api-keys",
        bearer_token=state.supabase_session.access_token,
        payload={"label": label},
        opener=opener,
    )


def revoke_api_key(
    *,
    api_url: str,
    api_key_id: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Revoke a manual org API key for the current org."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    organization = _require_current_org(state)
    return _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization.id}/api-keys/{api_key_id}",
        bearer_token=state.supabase_session.access_token,
        method="DELETE",
        opener=opener,
    )


def list_members(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> list[dict[str, Any]]:
    """List members of the current organization."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    organization = _require_current_org(state)
    payload = _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization.id}/members",
        bearer_token=state.supabase_session.access_token,
        opener=opener,
    )
    return list(payload["members"])


def invite_member(
    *,
    api_url: str,
    email: str,
    role: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Invite a member to the current organization."""
    state = _load_authenticated_state(api_url=api_url, environ=environ, opener=opener)
    organization = _require_current_org(state)
    return _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization.id}/invitations",
        bearer_token=state.supabase_session.access_token,
        payload={"email": email, "role": role},
        opener=opener,
    )


def resolve_default_api_key(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
) -> str | None:
    """Return the cached managed org API key for the current org, if any."""
    api_url = _normalize_api_url(api_url)
    state = load_cli_state(environ=environ)
    if not _api_urls_match(state.api_url, api_url):
        return None
    organization = _current_org(state)
    if organization is None:
        return None
    return organization.api_key


def _load_authenticated_state(
    *,
    api_url: str,
    environ: dict[str, str] | None,
    opener: Callable[..., Any],
) -> CliState:
    api_url = _normalize_api_url(api_url)
    state = load_cli_state(environ=environ)
    if not _api_urls_match(state.api_url, api_url) or state.supabase_session is None:
        raise DariCliAuthError(
            "No CLI login is available for this API URL. Run `dari auth login` first."
        )
    state = _refresh_supabase_session(state, api_url=api_url, opener=opener)
    save_cli_state(state, environ=environ)
    return state


def _bootstrap_and_select_current_org(
    state: CliState,
    *,
    api_url: str,
    opener: Callable[..., Any],
    preferred_org_id: str | None,
) -> CliState:
    refreshed = _refresh_supabase_session(state, api_url=api_url, opener=opener)
    payload = _management_request(
        api_url=api_url,
        path="/v1/me/bootstrap",
        bearer_token=refreshed.supabase_session.access_token,
        payload={},
        opener=opener,
    )
    refreshed = _sync_organizations(refreshed, payload["organizations"])
    selected_org_id = preferred_org_id or refreshed.current_org_id
    if selected_org_id is None or selected_org_id not in refreshed.organizations:
        if not payload["organizations"]:
            raise DariCliAuthError("No organizations are available for this account.")
        selected_org_id = payload["organizations"][0]["id"]
    return _ensure_current_org_key(
        refreshed,
        api_url=api_url,
        organization_id=selected_org_id,
        opener=opener,
    )


def _ensure_current_org_key(
    state: CliState,
    *,
    api_url: str,
    organization_id: str,
    opener: Callable[..., Any],
) -> CliState:
    if state.supabase_session is None:
        raise DariCliAuthError("No Supabase session is available.")
    issued = _management_request(
        api_url=api_url,
        path=f"/v1/organizations/{organization_id}/managed-cli-key/ensure",
        bearer_token=state.supabase_session.access_token,
        payload={"device_name": socket.gethostname()},
        opener=opener,
    )
    organization = state.organizations.get(organization_id)
    if organization is None:
        raise DariCliAuthError(
            f"Organization {organization_id!r} is not known locally."
        )
    state.current_org_id = organization_id
    state.organizations[organization_id] = StoredOrganization(
        id=organization.id,
        name=organization.name,
        slug=organization.slug,
        role=organization.role,
        api_key=str(issued["api_key"]),
    )
    return state


def _refresh_supabase_session(
    state: CliState,
    *,
    api_url: str,
    opener: Callable[..., Any],
) -> CliState:
    api_url = _normalize_api_url(api_url)
    if state.supabase_session is None:
        raise DariCliAuthError("No Supabase session is available.")
    auth_config = _fetch_auth_config(api_url, opener=opener)
    supabase = _build_supabase_client(auth_config)
    auth_response = supabase.auth.set_session(
        state.supabase_session.access_token,
        state.supabase_session.refresh_token,
    )
    session = auth_response.session or supabase.auth.get_session()
    if session is None or session.user is None or session.user.email is None:
        raise DariCliAuthError("Unable to refresh the Supabase session.")
    state.api_url = api_url
    state.supabase_session = _stored_session_from_supabase_session(session)
    return state


def _sync_organizations(
    state: CliState, organizations: list[dict[str, Any]]
) -> CliState:
    existing_keys = {
        organization_id: organization.api_key
        for organization_id, organization in state.organizations.items()
    }
    synced: dict[str, StoredOrganization] = {}
    for organization in organizations:
        organization_id = str(organization["id"])
        synced[organization_id] = StoredOrganization(
            id=organization_id,
            name=str(organization["name"]),
            slug=str(organization["slug"]),
            role=str(organization["role"]),
            api_key=existing_keys.get(organization_id),
        )
    state.organizations = synced
    if state.current_org_id not in synced:
        state.current_org_id = next(iter(synced), None)
    return state


def _upsert_organization(state: CliState, organization: dict[str, Any]) -> CliState:
    organization_id = str(organization["id"])
    existing = state.organizations.get(organization_id)
    state.organizations[organization_id] = StoredOrganization(
        id=organization_id,
        name=str(organization["name"]),
        slug=str(organization["slug"]),
        role=str(organization["role"]),
        api_key=None if existing is None else existing.api_key,
    )
    return state


def _current_org(state: CliState) -> StoredOrganization | None:
    if state.current_org_id is None:
        return None
    return state.organizations.get(state.current_org_id)


def _require_current_org(state: CliState) -> StoredOrganization:
    organization = _current_org(state)
    if organization is None:
        raise DariCliAuthError(
            "No current organization is selected. Run `dari org switch <org>`."
        )
    return organization


def _match_organization(
    organizations: list[dict[str, Any]],
    identifier: str,
) -> dict[str, Any] | None:
    for organization in organizations:
        if organization["id"] == identifier or organization["slug"] == identifier:
            return organization
    return None


def _fetch_auth_config(api_url: str, *, opener: Callable[..., Any]) -> dict[str, Any]:
    return _json_request(
        _join_url(api_url, "/v1/auth/config"),
        opener=opener,
    )


def _management_request(
    *,
    api_url: str,
    path: str,
    bearer_token: str,
    opener: Callable[..., Any],
    payload: dict[str, Any] | None = None,
    method: str | None = None,
) -> dict[str, Any]:
    headers = {"Authorization": f"Bearer {bearer_token}"}
    return _json_request(
        _join_url(api_url, path),
        opener=opener,
        payload=payload,
        headers=headers,
        method=method,
    )


def _json_request(
    url: str,
    *,
    opener: Callable[..., Any],
    payload: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    method: str | None = None,
) -> dict[str, Any]:
    request_headers = {"Accept": "application/json", **(headers or {})}
    data = None
    resolved_method = method or ("POST" if payload is not None else "GET")
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        request_headers["Content-Type"] = "application/json"

    request = Request(
        url,
        data=data,
        headers=request_headers,
        method=resolved_method,
    )
    try:
        with opener(request) as response:
            return json.loads(response.read().decode("utf-8"))
    except HTTPError as exc:
        detail = exc.reason
        if exc.fp is not None:
            try:
                payload = json.loads(exc.fp.read().decode("utf-8"))
            except Exception:
                payload = None
            if isinstance(payload, dict) and "detail" in payload:
                detail = str(payload["detail"])
        raise DariCliAuthError(str(detail)) from exc


def _join_url(api_url: str, path: str) -> str:
    return f"{api_url.rstrip('/')}{path}"


def _normalize_api_url(api_url: str) -> str:
    normalized = api_url.strip().rstrip("/")
    return normalized or api_url.strip()


def _api_urls_match(left: str | None, right: str) -> bool:
    if left is None:
        return False
    return _normalize_api_url(left) == _normalize_api_url(right)


def _build_supabase_client(auth_config: dict[str, Any]):
    return create_client(
        str(auth_config["supabase_url"]),
        str(auth_config["supabase_publishable_key"]),
        options=ClientOptions(
            flow_type="pkce",
            persist_session=False,
            auto_refresh_token=False,
        ),
    )


def _stored_session_from_supabase_session(session) -> StoredSupabaseSession:  # noqa: ANN001
    user = session.user
    display_name = None
    if user is not None and isinstance(user.user_metadata, dict):
        raw_display_name = user.user_metadata.get(
            "full_name"
        ) or user.user_metadata.get("name")
        if isinstance(raw_display_name, str) and raw_display_name.strip():
            display_name = raw_display_name.strip()
    return StoredSupabaseSession(
        access_token=str(session.access_token),
        refresh_token=str(session.refresh_token),
        expires_at=session.expires_at,
        user_id=str(user.id),
        email=str(user.email),
        display_name=display_name,
    )


@dataclass(slots=True)
class _CallbackResult:
    """OAuth callback result captured by the local HTTP server."""

    redirect_url: str
    state: str | None = None
    code: str | None = None
    error: str | None = None
    event: threading.Event = field(default_factory=threading.Event)

    def wait(self, *, timeout_seconds: float) -> "_CallbackResult":
        self.event.wait(timeout_seconds)
        return self


@contextmanager
def _run_local_callback_server() -> Iterator[_CallbackResult]:
    host = "127.0.0.1"
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind((host, 0))
        port = sock.getsockname()[1]

    result = _CallbackResult(redirect_url=f"http://{host}:{port}/callback")

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            parsed = urlparse(self.path)
            params = parse_qs(parsed.query)
            result.code = _first_query_param(params, "code")
            result.error = _first_query_param(
                params, "error_description"
            ) or _first_query_param(params, "error")
            result.state = _first_query_param(params, "state")
            body = (
                "Dari CLI login complete. You can close this tab.\n"
                if result.error is None
                else f"Dari CLI login failed: {result.error}\n"
            )
            self.send_response(200 if result.error is None else 400)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(body.encode("utf-8"))
            result.event.set()

        def log_message(self, format: str, *args) -> None:  # noqa: A003
            return None

    server = HTTPServer((host, port), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield result
    finally:
        server.shutdown()
        thread.join(timeout=1)


def _first_query_param(params: dict[str, list[str]], key: str) -> str | None:
    values = params.get(key)
    if not values:
        return None
    value = values[0].strip()
    return value or None

"""Browser auth and organization-management helpers for the Dari CLI."""

from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import socket
import threading
import time
from typing import Any, Callable, Iterator
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, quote, urlparse
from urllib.request import Request, urlopen
import webbrowser

import httpx
from supabase import ClientOptions, create_client
from supabase_auth.errors import AuthError, AuthRetryableError, UserDoesntExist
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
MANAGEMENT_REQUEST_TIMEOUT_SECONDS = 15.0
SUPABASE_AUTH_TIMEOUT_SECONDS = 15.0
SESSION_REFRESH_LEEWAY_SECONDS = 60


class DariCliCommandError(RuntimeError):
    """Raised for expected user-facing CLI command failures."""


class DariCliAuthError(DariCliCommandError):
    """Raised when CLI auth or org management fails."""


class DariCliNetworkError(DariCliCommandError):
    """Raised when a CLI command hits a retryable network or transport failure."""


class _JsonRequestHttpError(RuntimeError):
    """Internal structured HTTP error used for auth-aware retry logic."""

    def __init__(self, status_code: int, detail: str) -> None:
        super().__init__(detail)
        self.status_code = status_code
        self.detail = detail


@dataclass(frozen=True, slots=True)
class AuthStatus:
    """Current CLI auth status."""

    api_url: str | None
    email: str | None
    current_org: StoredOrganization | None


@dataclass(frozen=True, slots=True)
class _AuthenticatedState:
    """Current auth state plus whether this call reused the cached token."""

    state: CliState
    used_cached_access_token: bool


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
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    authenticated_state, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path="/v1/organizations",
        opener=opener,
    )
    state = authenticated_state.state
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
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    authenticated_state, created = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path="/v1/organizations",
        payload={"name": name},
        opener=opener,
    )
    state = _upsert_organization(authenticated_state.state, created)
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
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/api-keys",
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
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/api-keys",
        payload={"label": label},
        opener=opener,
    )
    return payload


def revoke_api_key(
    *,
    api_url: str,
    api_key_id: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Revoke a manual org API key for the current org."""
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/api-keys/{api_key_id}",
        method="DELETE",
        opener=opener,
    )
    return payload


def list_members(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> list[dict[str, Any]]:
    """List members of the current organization."""
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/members",
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
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/invitations",
        payload={"email": email, "role": role},
        opener=opener,
    )
    return payload


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


def list_credentials(
    *,
    api_url: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> list[dict[str, Any]]:
    """List credentials for the current organization."""
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/credentials",
        opener=opener,
    )
    return list(payload["credentials"])


def upsert_credential(
    *,
    api_url: str,
    name: str,
    value: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Create or update a credential for the current organization."""
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/credentials/{quote(name, safe='')}",
        method="PUT",
        payload={"value": value},
        opener=opener,
    )
    return payload


def delete_credential(
    *,
    api_url: str,
    name: str,
    environ: dict[str, str] | None = None,
    opener: Callable[..., Any] = urlopen,
) -> dict[str, Any]:
    """Delete a credential from the current organization."""
    authenticated_state = _load_authenticated_state(
        api_url=api_url,
        environ=environ,
        opener=opener,
    )
    organization = _require_current_org(authenticated_state.state)
    _, payload = _execute_management_request(
        authenticated_state=authenticated_state,
        api_url=api_url,
        environ=environ,
        path=f"/v1/organizations/{organization.id}/credentials/{quote(name, safe='')}",
        method="DELETE",
        opener=opener,
    )
    return payload


def _load_authenticated_state(
    *,
    api_url: str,
    environ: dict[str, str] | None,
    opener: Callable[..., Any],
) -> _AuthenticatedState:
    api_url = _normalize_api_url(api_url)
    state = load_cli_state(environ=environ)
    if not _api_urls_match(state.api_url, api_url) or state.supabase_session is None:
        raise DariCliAuthError(
            "No CLI login is available for this API URL. Run `dari auth login` first."
        )
    if _stored_session_needs_refresh(state.supabase_session):
        state = _refresh_supabase_session(state, api_url=api_url, opener=opener)
        save_cli_state(state, environ=environ)
        return _AuthenticatedState(state=state, used_cached_access_token=False)
    return _AuthenticatedState(state=state, used_cached_access_token=True)


def _execute_management_request(
    *,
    authenticated_state: _AuthenticatedState,
    api_url: str,
    environ: dict[str, str] | None,
    path: str,
    opener: Callable[..., Any],
    payload: dict[str, Any] | None = None,
    method: str | None = None,
) -> tuple[_AuthenticatedState, dict[str, Any]]:
    try:
        response_payload = _management_request(
            api_url=api_url,
            path=path,
            bearer_token=_require_access_token(authenticated_state.state),
            payload=payload,
            method=method,
            opener=opener,
        )
        return authenticated_state, response_payload
    except _JsonRequestHttpError as exc:
        if authenticated_state.used_cached_access_token and exc.status_code in {
            401,
            403,
        }:
            refreshed_state = _refresh_supabase_session(
                authenticated_state.state,
                api_url=api_url,
                opener=opener,
            )
            save_cli_state(refreshed_state, environ=environ)
            retried_state = _AuthenticatedState(
                state=refreshed_state,
                used_cached_access_token=False,
            )
            try:
                response_payload = _management_request(
                    api_url=api_url,
                    path=path,
                    bearer_token=_require_access_token(refreshed_state),
                    payload=payload,
                    method=method,
                    opener=opener,
                )
                return retried_state, response_payload
            except _JsonRequestHttpError as retry_exc:
                raise _translate_management_http_error(retry_exc) from retry_exc
        raise _translate_management_http_error(exc) from exc


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
    try:
        auth_response = supabase.auth.set_session(
            state.supabase_session.access_token,
            state.supabase_session.refresh_token,
        )
        session = auth_response.session or supabase.auth.get_session()
    except (httpx.TimeoutException, httpx.TransportError, AuthRetryableError) as exc:
        raise DariCliNetworkError(
            "Supabase session refresh failed. Try again."
        ) from exc
    except (AuthError, UserDoesntExist) as exc:
        raise DariCliAuthError("Unable to refresh the Supabase session.") from exc
    if session is None or session.user is None or session.user.email is None:
        raise DariCliAuthError("Unable to refresh the Supabase session.")
    state.api_url = api_url
    state.supabase_session = _stored_session_from_supabase_session(session)
    return state


def _stored_session_needs_refresh(session: StoredSupabaseSession) -> bool:
    if session.expires_at is None:
        return True
    return session.expires_at <= round(time.time()) + SESSION_REFRESH_LEEWAY_SECONDS


def _require_access_token(state: CliState) -> str:
    if state.supabase_session is None:
        raise DariCliAuthError("No Supabase session is available.")
    return state.supabase_session.access_token


def _translate_management_http_error(
    exc: _JsonRequestHttpError,
) -> DariCliCommandError:
    if exc.status_code in {401, 403}:
        return DariCliAuthError(exc.detail)
    return DariCliCommandError(exc.detail)


def _extract_http_error_detail(exc: HTTPError) -> str:
    detail = exc.reason
    if exc.fp is not None:
        try:
            payload = json.loads(exc.fp.read().decode("utf-8"))
        except Exception:
            payload = None
        if isinstance(payload, dict) and "detail" in payload:
            detail = str(payload["detail"])
    return str(detail)


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
        timeout_seconds=MANAGEMENT_REQUEST_TIMEOUT_SECONDS,
        network_error_message="Failed to fetch CLI auth configuration. Try again.",
        invalid_response_message="CLI auth configuration response was invalid.",
        http_error_cls=DariCliCommandError,
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
        timeout_seconds=MANAGEMENT_REQUEST_TIMEOUT_SECONDS,
        network_error_message="Agent Host management request failed. Try again.",
        invalid_response_message="Agent Host management request returned an invalid response.",
        structured_http_errors=True,
    )


def _json_request(
    url: str,
    *,
    opener: Callable[..., Any],
    payload: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    method: str | None = None,
    timeout_seconds: float,
    network_error_message: str,
    invalid_response_message: str,
    http_error_cls: type[DariCliCommandError] = DariCliCommandError,
    structured_http_errors: bool = False,
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
        with opener(request, timeout=timeout_seconds) as response:
            response_body = response.read().decode("utf-8")
    except HTTPError as exc:
        detail = _extract_http_error_detail(exc)
        if structured_http_errors:
            raise _JsonRequestHttpError(exc.code, detail) from exc
        raise http_error_cls(detail) from exc
    except (TimeoutError, socket.timeout, URLError) as exc:
        raise DariCliNetworkError(network_error_message) from exc

    if not response_body:
        return {}
    try:
        response_json = json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise DariCliCommandError(invalid_response_message) from exc
    if not isinstance(response_json, dict):
        raise DariCliCommandError(invalid_response_message)
    return response_json


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
            httpx_client=httpx.Client(timeout=SUPABASE_AUTH_TIMEOUT_SECONDS),
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

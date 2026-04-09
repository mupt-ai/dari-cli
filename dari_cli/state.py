"""Local CLI auth and organization state."""

from __future__ import annotations

from dataclasses import dataclass, field
import json
import os
from pathlib import Path
import stat
from typing import Any


STATE_SCHEMA_VERSION = 1
STATE_FILENAME = "state.json"


@dataclass(frozen=True, slots=True)
class StoredSupabaseSession:
    """Local Supabase session persisted by the CLI."""

    access_token: str
    refresh_token: str
    expires_at: int | None
    user_id: str
    email: str
    display_name: str | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "access_token": self.access_token,
            "refresh_token": self.refresh_token,
            "expires_at": self.expires_at,
            "user_id": self.user_id,
            "email": self.email,
            "display_name": self.display_name,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "StoredSupabaseSession":
        return cls(
            access_token=str(payload["access_token"]),
            refresh_token=str(payload["refresh_token"]),
            expires_at=(
                int(payload["expires_at"])
                if payload.get("expires_at") is not None
                else None
            ),
            user_id=str(payload["user_id"]),
            email=str(payload["email"]),
            display_name=_optional_string(payload.get("display_name")),
        )


@dataclass(frozen=True, slots=True)
class StoredOrganization:
    """One locally known organization plus its managed CLI API key."""

    id: str
    name: str
    slug: str
    role: str
    api_key: str | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "name": self.name,
            "slug": self.slug,
            "role": self.role,
            "api_key": self.api_key,
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "StoredOrganization":
        return cls(
            id=str(payload["id"]),
            name=str(payload["name"]),
            slug=str(payload["slug"]),
            role=str(payload.get("role", "member")),
            api_key=_optional_string(payload.get("api_key")),
        )


@dataclass(slots=True)
class CliState:
    """Complete local CLI state."""

    api_url: str | None = None
    supabase_session: StoredSupabaseSession | None = None
    current_org_id: str | None = None
    organizations: dict[str, StoredOrganization] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema_version": STATE_SCHEMA_VERSION,
            "api_url": self.api_url,
            "supabase_session": (
                None if self.supabase_session is None else self.supabase_session.to_dict()
            ),
            "current_org_id": self.current_org_id,
            "organizations": {
                organization_id: organization.to_dict()
                for organization_id, organization in sorted(self.organizations.items())
            },
        }

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "CliState":
        return cls(
            api_url=_optional_string(payload.get("api_url")),
            supabase_session=(
                None
                if payload.get("supabase_session") is None
                else StoredSupabaseSession.from_dict(payload["supabase_session"])
            ),
            current_org_id=_optional_string(payload.get("current_org_id")),
            organizations={
                organization_id: StoredOrganization.from_dict(organization_payload)
                for organization_id, organization_payload in (
                    payload.get("organizations") or {}
                ).items()
            },
        )


def load_cli_state(*, environ: dict[str, str] | None = None) -> CliState:
    """Load the local CLI state, returning an empty state when missing."""
    path = get_cli_state_path(environ=environ)
    if not path.exists():
        return CliState()
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("schema_version") != STATE_SCHEMA_VERSION:
        raise RuntimeError(
            f"Unsupported Dari CLI state schema version: {payload.get('schema_version')!r}."
        )
    return CliState.from_dict(payload)


def save_cli_state(state: CliState, *, environ: dict[str, str] | None = None) -> None:
    """Persist the local CLI state with user-only permissions."""
    path = get_cli_state_path(environ=environ)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(state.to_dict(), indent=2, sort_keys=True), encoding="utf-8")
    if os.name != "nt":
        path.chmod(stat.S_IRUSR | stat.S_IWUSR)


def clear_cli_state(*, environ: dict[str, str] | None = None) -> None:
    """Delete any persisted CLI state."""
    path = get_cli_state_path(environ=environ)
    if path.exists():
        path.unlink()


def get_cli_state_path(*, environ: dict[str, str] | None = None) -> Path:
    """Return the local CLI state file path."""
    values = os.environ if environ is None else environ
    explicit_dir = _optional_string(values.get("DARI_CONFIG_DIR"))
    if explicit_dir is not None:
        return Path(explicit_dir).expanduser() / STATE_FILENAME

    xdg_dir = _optional_string(values.get("XDG_CONFIG_HOME"))
    if xdg_dir is not None:
        return Path(xdg_dir).expanduser() / "dari" / STATE_FILENAME

    appdata = _optional_string(values.get("APPDATA"))
    if appdata is not None:
        return Path(appdata).expanduser() / "dari" / STATE_FILENAME

    return Path.home() / ".config" / "dari" / STATE_FILENAME


def _optional_string(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None

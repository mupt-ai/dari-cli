"""Command-line entrypoint for Dari."""

from __future__ import annotations

import argparse
import getpass
import json
import os
import sys
from collections.abc import Sequence
from pathlib import Path

from .deploy import DeployConfigurationError, deploy_checkout, prepare_deploy_flow
from .management import (
    DEFAULT_API_URL,
    DariCliCommandError,
    create_execution_backend,
    create_api_key,
    create_organization,
    delete_credential,
    get_auth_status,
    invite_member,
    list_execution_backends,
    list_api_keys,
    list_credentials,
    list_members,
    list_organizations,
    login,
    logout,
    resolve_default_api_key,
    revoke_api_key,
    switch_organization,
    upsert_credential,
)
from .manifest import ManifestValidationError, load_manifest


def build_parser() -> argparse.ArgumentParser:
    """Build the Dari command-line parser."""
    parser = argparse.ArgumentParser(prog="dari", description="Dari CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)

    deploy_parser = subparsers.add_parser(
        "deploy",
        help="Package the current checkout and publish it to Dari.",
    )
    deploy_parser.add_argument(
        "repo_root",
        nargs="?",
        default=".",
        help="Path to the repo checkout to deploy.",
    )
    deploy_parser.add_argument(
        "--api-url",
        default=os.environ.get("DARI_API_URL", DEFAULT_API_URL),
        help=argparse.SUPPRESS,
    )
    deploy_parser.add_argument(
        "--api-key",
        default=os.environ.get("DARI_API_KEY"),
        help="Bearer token for the Dari API.",
    )
    deploy_parser.add_argument(
        "--agent-id",
        default=os.environ.get("DARI_AGENT_ID"),
        help="Existing agent ID to publish a new version for.",
    )
    deploy_parser.add_argument(
        "--execution-backend-id",
        default=os.environ.get("DARI_EXECUTION_BACKEND_ID"),
        help="Execution backend ID to pin for harness 'pi' publishes.",
    )
    deploy_parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the prepared publish request instead of sending it.",
    )
    deploy_parser.set_defaults(handler=_handle_deploy)

    auth_parser = subparsers.add_parser("auth", help="Manage browser login state")
    auth_subparsers = auth_parser.add_subparsers(dest="auth_command", required=True)

    auth_login_parser = auth_subparsers.add_parser(
        "login",
        help="Open the browser and log in with Supabase auth.",
    )
    _add_api_url_argument(auth_login_parser)
    auth_login_parser.set_defaults(handler=_handle_auth_login)

    auth_logout_parser = auth_subparsers.add_parser(
        "logout",
        help="Clear local browser login state.",
    )
    _add_api_url_argument(auth_logout_parser)
    auth_logout_parser.set_defaults(handler=_handle_auth_logout)

    auth_status_parser = auth_subparsers.add_parser(
        "status",
        help="Show the current browser login and org selection.",
    )
    _add_api_url_argument(auth_status_parser)
    auth_status_parser.set_defaults(handler=_handle_auth_status)

    org_parser = subparsers.add_parser("org", help="Manage organizations")
    org_subparsers = org_parser.add_subparsers(dest="org_command", required=True)

    org_list_parser = org_subparsers.add_parser("list", help="List available orgs")
    _add_api_url_argument(org_list_parser)
    org_list_parser.set_defaults(handler=_handle_org_list)

    org_create_parser = org_subparsers.add_parser("create", help="Create a new org")
    _add_api_url_argument(org_create_parser)
    org_create_parser.add_argument("name", help="Display name for the new org")
    org_create_parser.set_defaults(handler=_handle_org_create)

    org_switch_parser = org_subparsers.add_parser(
        "switch",
        help="Switch the current org by slug or ID",
    )
    _add_api_url_argument(org_switch_parser)
    org_switch_parser.add_argument("organization", help="Org slug or ID")
    org_switch_parser.set_defaults(handler=_handle_org_switch)

    org_members_parser = org_subparsers.add_parser(
        "members",
        help="List members in the current org",
    )
    _add_api_url_argument(org_members_parser)
    org_members_parser.set_defaults(handler=_handle_org_members)

    org_invite_parser = org_subparsers.add_parser(
        "invite",
        help="Invite a user to the current org",
    )
    _add_api_url_argument(org_invite_parser)
    org_invite_parser.add_argument("email", help="Email to invite")
    org_invite_parser.add_argument(
        "--role",
        default="member",
        choices=["owner", "admin", "member"],
        help="Membership role for the invite.",
    )
    org_invite_parser.set_defaults(handler=_handle_org_invite)

    api_keys_parser = subparsers.add_parser(
        "api-keys",
        help="Manage API keys for the current org",
    )
    api_keys_subparsers = api_keys_parser.add_subparsers(
        dest="api_keys_command",
        required=True,
    )

    api_keys_list_parser = api_keys_subparsers.add_parser(
        "list",
        help="List API keys for the current org",
    )
    _add_api_url_argument(api_keys_list_parser)
    api_keys_list_parser.set_defaults(handler=_handle_api_keys_list)

    api_keys_create_parser = api_keys_subparsers.add_parser(
        "create",
        help="Create a new manual API key for the current org",
    )
    _add_api_url_argument(api_keys_create_parser)
    api_keys_create_parser.add_argument("--name", required=True, help="Key label")
    api_keys_create_parser.set_defaults(handler=_handle_api_keys_create)

    api_keys_revoke_parser = api_keys_subparsers.add_parser(
        "revoke",
        help="Revoke an API key for the current org",
    )
    _add_api_url_argument(api_keys_revoke_parser)
    api_keys_revoke_parser.add_argument("key_id", help="Organization API key ID")
    api_keys_revoke_parser.set_defaults(handler=_handle_api_keys_revoke)

    credentials_parser = subparsers.add_parser(
        "credentials",
        help="Manage runtime credentials for the current org",
    )
    credentials_subparsers = credentials_parser.add_subparsers(
        dest="credentials_command",
        required=True,
    )

    credentials_list_parser = credentials_subparsers.add_parser(
        "list",
        help="List stored credential names for the current org",
    )
    _add_api_url_argument(credentials_list_parser)
    credentials_list_parser.set_defaults(handler=_handle_credentials_list)

    credentials_add_parser = credentials_subparsers.add_parser(
        "add",
        help="Create or update a runtime credential for the current org",
    )
    _add_api_url_argument(credentials_add_parser)
    credentials_add_parser.add_argument("name", help="Credential/env var name")
    credentials_add_parser.add_argument(
        "value",
        nargs="?",
        help="Credential value. Omit to prompt securely.",
    )
    credentials_add_parser.add_argument(
        "--value-stdin",
        action="store_true",
        help="Read the credential value from standard input.",
    )
    credentials_add_parser.set_defaults(handler=_handle_credentials_add)

    credentials_remove_parser = credentials_subparsers.add_parser(
        "remove",
        help="Delete a runtime credential from the current org",
    )
    _add_api_url_argument(credentials_remove_parser)
    credentials_remove_parser.add_argument("name", help="Credential/env var name")
    credentials_remove_parser.set_defaults(handler=_handle_credentials_remove)

    execution_backends_parser = subparsers.add_parser(
        "execution-backends",
        help="Manage execution backends for the current org",
    )
    execution_backends_subparsers = execution_backends_parser.add_subparsers(
        dest="execution_backends_command",
        required=True,
    )

    execution_backends_list_parser = execution_backends_subparsers.add_parser(
        "list",
        help="List execution backends for the current org",
    )
    _add_api_url_argument(execution_backends_list_parser)
    execution_backends_list_parser.set_defaults(handler=_handle_execution_backends_list)

    execution_backends_create_parser = execution_backends_subparsers.add_parser(
        "create",
        help="Create an execution backend for the current org",
    )
    _add_api_url_argument(execution_backends_create_parser)
    execution_backends_create_parser.add_argument(
        "--name",
        required=True,
        help="Execution backend label",
    )
    execution_backends_create_parser.add_argument(
        "--provider",
        required=True,
        help="Execution backend provider, for example e2b.",
    )
    execution_backends_create_parser.add_argument(
        "--config-json",
        help="Provider config as a JSON object. Omit to use the generic API-key convenience path.",
    )
    execution_backends_create_parser.add_argument(
        "--config-json-stdin",
        action="store_true",
        help="Read provider config JSON from standard input.",
    )
    execution_backends_create_parser.add_argument(
        "--api-key",
        help="Provider API key convenience input. Omit to prompt securely.",
    )
    execution_backends_create_parser.add_argument(
        "--api-key-stdin",
        action="store_true",
        help="Read the provider API key from standard input.",
    )
    execution_backends_create_parser.set_defaults(
        handler=_handle_execution_backends_create
    )

    manifest_parser = subparsers.add_parser(
        "manifest",
        help="Inspect and validate repo-root dari.yml files",
    )
    manifest_subparsers = manifest_parser.add_subparsers(
        dest="manifest_command",
        required=True,
    )

    validate_parser = manifest_subparsers.add_parser(
        "validate",
        help="Validate the repo-root dari.yml file",
    )
    validate_parser.add_argument(
        "repo_root",
        nargs="?",
        default=".",
        help="Repository root that contains dari.yml",
    )
    validate_parser.add_argument(
        "--json",
        action="store_true",
        help="Print the normalized manifest JSON on success",
    )
    validate_parser.set_defaults(handler=_run_manifest_validate)

    return parser


def _add_api_url_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--api-url",
        default=os.environ.get("DARI_API_URL", DEFAULT_API_URL),
        help=argparse.SUPPRESS,
    )


def _run_manifest_validate(args: argparse.Namespace) -> int:
    repo_root = Path(args.repo_root)
    manifest_path = repo_root / "dari.yml"

    if repo_root.exists() and not repo_root.is_dir():
        print(f"Repository root must be a directory: {repo_root}", file=sys.stderr)
        return 1

    try:
        manifest = load_manifest(repo_root)
    except FileNotFoundError:
        print(f"Manifest file not found: {manifest_path}", file=sys.stderr)
        return 1
    except ManifestValidationError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    if args.json:
        print(json.dumps(manifest.to_dict(), indent=2, sort_keys=True))
    else:
        print(f"Validated {manifest_path}: {manifest.name} ({manifest.harness})")
    return 0


def main(argv: Sequence[str] | None = None) -> int:
    """Run the Dari CLI."""
    parser = build_parser()
    args = parser.parse_args(list(argv) if argv is not None else None)
    try:
        return args.handler(args)
    except DariCliCommandError as exc:
        print(str(exc), file=sys.stderr)
        return 1


def _handle_deploy(args: argparse.Namespace) -> int:
    try:
        prepared = prepare_deploy_flow(
            args.repo_root,
            agent_id=args.agent_id,
            execution_backend_id=args.execution_backend_id,
            api_url=args.api_url,
            environ=os.environ,
        )
    except DeployConfigurationError as exc:
        raise SystemExit(str(exc)) from exc

    if args.dry_run:
        print(json.dumps(prepared.to_dict(), indent=2, sort_keys=True))
        return 0

    api_key = args.api_key or resolve_default_api_key(
        api_url=args.api_url,
        environ=os.environ,
    )
    if not api_key:
        raise SystemExit(
            "DARI_API_KEY is required unless --dry-run is set or CLI login has selected an organization."
        )

    response = deploy_checkout(
        args.repo_root,
        api_url=args.api_url,
        api_key=api_key,
        agent_id=args.agent_id,
        execution_backend_id=args.execution_backend_id,
        environ=os.environ,
    )
    print(json.dumps(response, indent=2, sort_keys=True))
    return 0


def _handle_auth_login(args: argparse.Namespace) -> int:
    state = login(api_url=args.api_url, environ=os.environ)
    current_org = (
        None
        if state.current_org_id is None
        else state.organizations.get(state.current_org_id)
    )
    print(
        json.dumps(
            {
                "api_url": state.api_url,
                "email": (
                    None
                    if state.supabase_session is None
                    else state.supabase_session.email
                ),
                "current_org": (None if current_org is None else current_org.to_dict()),
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


def _handle_auth_logout(args: argparse.Namespace) -> int:
    logout(api_url=args.api_url, environ=os.environ)
    print(json.dumps({"logged_out": True}, indent=2, sort_keys=True))
    return 0


def _handle_auth_status(args: argparse.Namespace) -> int:
    status = get_auth_status(api_url=args.api_url, environ=os.environ)
    print(
        json.dumps(
            {
                "api_url": status.api_url,
                "email": status.email,
                "current_org": (
                    None if status.current_org is None else status.current_org.to_dict()
                ),
                "logged_in": status.email is not None,
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


def _handle_org_list(args: argparse.Namespace) -> int:
    state, organizations = list_organizations(api_url=args.api_url, environ=os.environ)
    print(
        json.dumps(
            {
                "current_org_id": state.current_org_id,
                "organizations": organizations,
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


def _handle_org_create(args: argparse.Namespace) -> int:
    state = create_organization(
        api_url=args.api_url,
        name=args.name,
        environ=os.environ,
    )
    current_org = state.organizations[state.current_org_id]
    print(
        json.dumps(
            {
                "current_org_id": state.current_org_id,
                "organization": current_org.to_dict(),
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


def _handle_org_switch(args: argparse.Namespace) -> int:
    state = switch_organization(
        api_url=args.api_url,
        identifier=args.organization,
        environ=os.environ,
    )
    current_org = state.organizations[state.current_org_id]
    print(
        json.dumps(
            {
                "current_org_id": state.current_org_id,
                "organization": current_org.to_dict(),
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


def _handle_org_members(args: argparse.Namespace) -> int:
    members = list_members(api_url=args.api_url, environ=os.environ)
    print(json.dumps({"members": members}, indent=2, sort_keys=True))
    return 0


def _handle_org_invite(args: argparse.Namespace) -> int:
    invitation = invite_member(
        api_url=args.api_url,
        email=args.email,
        role=args.role,
        environ=os.environ,
    )
    print(json.dumps(invitation, indent=2, sort_keys=True))
    return 0


def _handle_api_keys_list(args: argparse.Namespace) -> int:
    api_keys = list_api_keys(api_url=args.api_url, environ=os.environ)
    print(json.dumps({"api_keys": api_keys}, indent=2, sort_keys=True))
    return 0


def _handle_api_keys_create(args: argparse.Namespace) -> int:
    created = create_api_key(
        api_url=args.api_url,
        label=args.name,
        environ=os.environ,
    )
    print(json.dumps(created, indent=2, sort_keys=True))
    return 0


def _handle_api_keys_revoke(args: argparse.Namespace) -> int:
    revoked = revoke_api_key(
        api_url=args.api_url,
        api_key_id=args.key_id,
        environ=os.environ,
    )
    print(json.dumps(revoked, indent=2, sort_keys=True))
    return 0


def _handle_credentials_list(args: argparse.Namespace) -> int:
    credentials = list_credentials(api_url=args.api_url, environ=os.environ)
    print(json.dumps({"credentials": credentials}, indent=2, sort_keys=True))
    return 0


def _handle_credentials_add(args: argparse.Namespace) -> int:
    value = _resolve_credential_value(args)
    stored = upsert_credential(
        api_url=args.api_url,
        name=args.name,
        value=value,
        environ=os.environ,
    )
    print(json.dumps(stored, indent=2, sort_keys=True))
    return 0


def _handle_credentials_remove(args: argparse.Namespace) -> int:
    deleted = delete_credential(
        api_url=args.api_url,
        name=args.name,
        environ=os.environ,
    )
    print(json.dumps(deleted, indent=2, sort_keys=True))
    return 0


def _handle_execution_backends_list(args: argparse.Namespace) -> int:
    execution_backends = list_execution_backends(
        api_url=args.api_url,
        environ=os.environ,
    )
    print(
        json.dumps({"execution_backends": execution_backends}, indent=2, sort_keys=True)
    )
    return 0


def _handle_execution_backends_create(args: argparse.Namespace) -> int:
    created = create_execution_backend(
        api_url=args.api_url,
        name=args.name,
        provider=args.provider,
        config=_resolve_execution_backend_config(args),
        environ=os.environ,
    )
    print(json.dumps(created, indent=2, sort_keys=True))
    return 0


def _resolve_credential_value(args: argparse.Namespace) -> str:
    if args.value is not None and args.value_stdin:
        raise SystemExit("Pass either VALUE or --value-stdin, not both.")
    if args.value_stdin:
        value = sys.stdin.read().removesuffix("\n").removesuffix("\r")
    elif args.value is not None:
        print(
            (
                "Warning: passing credential values on the command line can expose "
                "them via shell history and process arguments."
            ),
            file=sys.stderr,
        )
        value = args.value
    else:
        value = getpass.getpass(f"{args.name}: ")
    if value == "":
        raise SystemExit("Credential value must be non-empty.")
    return value


def _resolve_execution_backend_api_key(args: argparse.Namespace) -> str:
    if args.api_key is not None and args.api_key_stdin:
        raise SystemExit("Pass either --api-key or --api-key-stdin, not both.")
    if args.api_key_stdin:
        value = sys.stdin.read().removesuffix("\n").removesuffix("\r")
    elif args.api_key is not None:
        print(
            (
                "Warning: passing execution backend API keys on the command line can expose "
                "them via shell history and process arguments."
            ),
            file=sys.stderr,
        )
        value = args.api_key
    else:
        value = getpass.getpass("API key: ")
    if value == "":
        raise SystemExit("Execution backend API key must be non-empty.")
    return value


def _resolve_execution_backend_config(args: argparse.Namespace) -> dict[str, object]:
    raw_config = _resolve_optional_execution_backend_config_json(args)
    if raw_config is not None:
        if args.api_key is not None or args.api_key_stdin:
            raise SystemExit(
                "Pass either provider config JSON or API key input, not both."
            )
        return raw_config
    return {"api_key": _resolve_execution_backend_api_key(args)}


def _resolve_optional_execution_backend_config_json(
    args: argparse.Namespace,
) -> dict[str, object] | None:
    if args.config_json is not None and args.config_json_stdin:
        raise SystemExit("Pass either --config-json or --config-json-stdin, not both.")
    if args.config_json_stdin:
        raw_json = sys.stdin.read().removesuffix("\n").removesuffix("\r")
    elif args.config_json is not None:
        print(
            (
                "Warning: passing execution backend config on the command line can expose "
                "it via shell history and process arguments."
            ),
            file=sys.stderr,
        )
        raw_json = args.config_json
    else:
        return None

    if raw_json == "":
        raise SystemExit("Execution backend config JSON must be non-empty.")
    try:
        parsed = json.loads(raw_json)
    except json.JSONDecodeError as exc:
        raise SystemExit(
            f"Execution backend config must be valid JSON: {exc.msg}."
        ) from exc
    if not isinstance(parsed, dict):
        raise SystemExit("Execution backend config must decode to a JSON object.")
    return parsed


if __name__ == "__main__":
    raise SystemExit(main())

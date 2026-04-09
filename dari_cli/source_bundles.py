"""Local source bundle packaging for the OSS Dari CLI."""

from __future__ import annotations

import gzip
import hashlib
import io
import os
import shutil
import stat
import subprocess
import tarfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Iterable, Mapping

CHUNK_SIZE = 64 * 1024
EXCLUDED_FALLBACK_DIRECTORY_NAMES = {
    ".dari",
    ".git",
    ".pytest_cache",
    ".venv",
    "__pycache__",
    "node_modules",
}
EXCLUDED_FALLBACK_FILE_NAMES = {".DS_Store"}
SourceBundleMetadataValue = str | bool


@dataclass(frozen=True)
class BuiltSourceBundleArchive:
    """Deterministic archive bytes plus bundle metadata."""

    content: bytes
    archive_sha256: str
    included_paths: tuple[str, ...]


@dataclass(frozen=True)
class GitSourceBundleMetadata:
    """Best-effort Git provenance for a deploy root."""

    git_commit_sha: str | None = None
    git_ref: str | None = None
    git_dirty: bool | None = None


def build_source_bundle_archive(deploy_root: Path) -> BuiltSourceBundleArchive:
    """Build a deterministic `.tar.gz` archive from a deploy root."""
    resolved_root = deploy_root.resolve(strict=True)
    if not resolved_root.is_dir():
        raise NotADirectoryError(f"{deploy_root} is not a directory.")

    selected_paths = tuple(_select_snapshot_paths(resolved_root))
    if "dari.yml" not in selected_paths:
        raise ValueError("Deploy root must contain a top-level dari.yml file.")

    buffer = io.BytesIO()
    with gzip.GzipFile(
        filename="",
        mode="wb",
        fileobj=buffer,
        mtime=0,
    ) as gzip_file:
        with tarfile.open(
            fileobj=gzip_file, mode="w", format=tarfile.PAX_FORMAT
        ) as archive:
            for relative_path in selected_paths:
                full_path = resolved_root / relative_path
                tar_info, file_object = _build_tar_member(
                    full_path, PurePosixPath(relative_path)
                )
                with file_object:
                    archive.addfile(tar_info, fileobj=file_object)

    content = buffer.getvalue()
    return BuiltSourceBundleArchive(
        content=content,
        archive_sha256=_sha256_bytes(content),
        included_paths=selected_paths,
    )


def collect_source_bundle_metadata(
    deploy_root: Path,
    *,
    environ: Mapping[str, str] | None = None,
) -> dict[str, SourceBundleMetadataValue]:
    """Collect best-effort Git and CI metadata for a deploy root."""
    env = os.environ if environ is None else environ
    metadata: dict[str, SourceBundleMetadataValue] = {
        "origin": _detect_source_bundle_origin(env),
    }

    git_metadata = _collect_git_metadata(deploy_root)
    git_commit_sha = git_metadata.git_commit_sha or env.get("GITHUB_SHA")
    if git_commit_sha is not None:
        metadata["git_commit_sha"] = git_commit_sha

    git_ref = git_metadata.git_ref or env.get("GITHUB_REF")
    if git_ref is not None:
        metadata["git_ref"] = git_ref

    if git_metadata.git_dirty is not None:
        metadata["git_dirty"] = git_metadata.git_dirty

    if env.get("GITHUB_ACTIONS") == "true":
        metadata["ci_provider"] = "github_actions"
        github_run_id = env.get("GITHUB_RUN_ID")
        if github_run_id:
            metadata["github_run_id"] = github_run_id

        server_url = env.get("GITHUB_SERVER_URL")
        repository = env.get("GITHUB_REPOSITORY")
        if server_url and repository and github_run_id:
            metadata["ci_run_url"] = (
                f"{server_url.rstrip('/')}/{repository}/actions/runs/{github_run_id}"
            )

    return metadata


def _select_snapshot_paths(deploy_root: Path) -> list[str]:
    git_paths = _git_snapshot_paths(deploy_root)
    if git_paths is not None:
        return git_paths

    selected_paths: list[str] = []
    for current_root, directory_names, file_names in os.walk(deploy_root):
        directory_names[:] = sorted(
            name
            for name in directory_names
            if name not in EXCLUDED_FALLBACK_DIRECTORY_NAMES
        )

        root_path = Path(current_root)
        for file_name in sorted(file_names):
            if file_name in EXCLUDED_FALLBACK_FILE_NAMES:
                continue

            full_path = root_path / file_name
            relative_path = full_path.relative_to(deploy_root)
            _ensure_allowed_snapshot_path(full_path, relative_path)
            selected_paths.append(relative_path.as_posix())

    return selected_paths


def _git_snapshot_paths(deploy_root: Path) -> list[str] | None:
    repo_root_text = _run_git(
        deploy_root, ["rev-parse", "--show-toplevel"], check=False
    )
    if repo_root_text is None:
        return None

    repo_root = Path(repo_root_text).resolve()
    relative_prefix = deploy_root.resolve().relative_to(repo_root)
    git_args = [
        "ls-files",
        "--cached",
        "--others",
        "--exclude-standard",
        "-z",
    ]
    if relative_prefix != Path("."):
        git_args.extend(["--", relative_prefix.as_posix()])

    output = _run_git(repo_root, git_args, check=True, text=False)
    if output is None:
        return None

    selected_paths: list[str] = []
    prefix = (
        PurePosixPath(relative_prefix.as_posix())
        if relative_prefix != Path(".")
        else None
    )
    for raw_path in output.split(b"\x00"):
        if not raw_path:
            continue
        repo_relative = PurePosixPath(raw_path.decode("utf-8"))
        if prefix is not None:
            try:
                relative_path = repo_relative.relative_to(prefix)
            except ValueError:
                continue
        else:
            relative_path = repo_relative

        full_path = deploy_root / Path(relative_path)
        _ensure_allowed_snapshot_path(full_path, Path(relative_path))
        selected_paths.append(relative_path.as_posix())

    selected_paths.sort()
    return selected_paths


def _build_tar_member(
    full_path: Path, relative_path: PurePosixPath
) -> tuple[tarfile.TarInfo, object]:
    _ensure_allowed_snapshot_path(full_path, Path(relative_path))
    stat_result = full_path.stat()
    tar_info = tarfile.TarInfo(relative_path.as_posix())
    tar_info.size = stat_result.st_size
    tar_info.mode = 0o755 if _is_executable(stat_result.st_mode) else 0o644
    tar_info.mtime = 0
    tar_info.uid = 0
    tar_info.gid = 0
    tar_info.uname = ""
    tar_info.gname = ""
    return tar_info, full_path.open("rb")


def _collect_git_metadata(deploy_root: Path) -> GitSourceBundleMetadata:
    repo_root_text = _run_git(
        deploy_root, ["rev-parse", "--show-toplevel"], check=False
    )
    if repo_root_text is None:
        return GitSourceBundleMetadata()

    repo_root = Path(repo_root_text).resolve()
    relative_prefix = deploy_root.resolve().relative_to(repo_root)
    commit_sha = _run_git(repo_root, ["rev-parse", "HEAD"], check=True)
    branch = _run_git(
        repo_root, ["symbolic-ref", "--quiet", "--short", "HEAD"], check=False
    )

    dirty_args = ["status", "--porcelain", "--untracked-files=normal", "-z"]
    if relative_prefix != Path("."):
        dirty_args.extend(["--", relative_prefix.as_posix()])
    dirty_output = _run_git(repo_root, dirty_args, check=True, text=False)

    return GitSourceBundleMetadata(
        git_commit_sha=commit_sha,
        git_ref=branch,
        git_dirty=bool(dirty_output),
    )


def _ensure_allowed_snapshot_path(full_path: Path, relative_path: Path) -> None:
    if full_path.is_symlink():
        raise ValueError(f"Source bundle cannot include symlink {relative_path}.")

    normalized = PurePosixPath(relative_path.as_posix())
    if normalized.is_absolute() or ".." in normalized.parts:
        raise ValueError(f"Invalid source bundle path {relative_path!s}.")

    file_mode = full_path.stat().st_mode
    if not stat.S_ISREG(file_mode):
        raise ValueError(
            f"Source bundle can only include regular files, found {relative_path!s}."
        )


def _run_git(
    cwd: Path, args: Iterable[str], *, check: bool, text: bool = True
) -> str | bytes | None:
    if shutil.which("git") is None:
        return None

    completed = subprocess.run(
        ["git", *args],
        cwd=str(cwd),
        check=False,
        capture_output=True,
        text=text,
    )
    if completed.returncode != 0:
        if check:
            stderr = (
                completed.stderr.decode("utf-8")
                if isinstance(completed.stderr, bytes)
                else completed.stderr
            )
            raise RuntimeError(stderr.strip() or "git command failed")
        return None

    output = completed.stdout
    if text:
        assert isinstance(output, str)
        return output.strip()
    assert isinstance(output, bytes)
    return output


def _detect_source_bundle_origin(env: Mapping[str, str]) -> str:
    if env.get("GITHUB_ACTIONS") == "true" or env.get("CI") == "true":
        return "ci"
    return "local_cli"


def _sha256_bytes(payload: bytes) -> str:
    hasher = hashlib.sha256()
    for index in range(0, len(payload), CHUNK_SIZE):
        hasher.update(payload[index : index + CHUNK_SIZE])
    return hasher.hexdigest()


def _is_executable(file_mode: int) -> bool:
    return bool(file_mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH))

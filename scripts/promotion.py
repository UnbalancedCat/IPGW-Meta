#!/usr/bin/env python3
"""Closed-world verifier used by the no-build v1.0.0 promotion workflow."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import io
import os
import re
import stat
import subprocess
import sys
import struct
import zlib
import zipfile
from pathlib import Path, PurePosixPath
from typing import BinaryIO, Iterable

PLAN_ID = "IPGW-META-V1"
REVISION = "2026-08-28-r2"
VERSION = "v1.0.0"
REPOSITORY_ID = 1186323753
CANDIDATE_WORKFLOW = ".github/workflows/candidate.yml"
MAX_JSON_BYTES = 64 * 1024
MAX_ARTIFACT_BYTES = 1024 * 1024 * 1024
MAX_BINARY_BYTES = 64 * 1024 * 1024
MAX_ARCHIVE_BYTES = 100 * 1024 * 1024
MAX_INSTALLER_BYTES = 4 * 1024 * 1024
BUNDLE_STATIC_FILES = {"LICENSE", "launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"}
TARGETS = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64")
MAX_PATH_BYTES = 4096
HEX40 = re.compile(r"^[0-9a-f]{40}$")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
CANDIDATE_ID = re.compile(r"^v1\.0\.0-([0-9a-f]{12})-([1-9][0-9]*)\.([1-9][0-9]*)$")
EVIDENCE_ID = re.compile(r"^EVID-([0-9]{4})([0-9]{2})([0-9]{2})-([0-9]{3})$")
RFC3339_UTC = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$")

LOCK_KEYS = [
    "schema_version", "plan_id", "revision", "version", "candidate_id",
    "source_commit", "source_tree", "workflow", "artifact",
    "candidate_set_sha256", "release_manifest_sha256", "build_input_sha256",
    "attestation_subjects", "evidence_ids", "release_notes_sha256",
]
WORKFLOW_KEYS = ["repository_id", "path", "run_id", "run_attempt"]
ARTIFACT_KEYS = ["id", "name", "digest"]
SUBJECT_KEYS = ["name", "sha256"]
EVIDENCE_KEYS = [
    "schema_version", "plan_id", "revision", "evidence_id", "candidate_id",
    "candidate_set_sha256", "source_commit", "platform", "testbed",
    "network_type", "auth_method", "suite", "result", "capability_before",
    "capability_after", "started_at", "finished_at", "bundle_sha256",
]
MANIFEST_KEYS = [
    "schema_version", "plan_id", "revision", "version", "candidate_id",
    "source_commit", "source_tree", "workflow_run_id", "workflow_run_attempt",
    "toolchain", "build_input_sha256", "release_assets", "test_tools",
    "live_gate_targets",
]
TOOLCHAIN_KEYS = [
    "go_version", "go_toolchain", "host_platform", "cgo_enabled", "goamd64",
    "goarm64", "source_date_epoch", "build_recipe",
]
ASSET_KEYS = ["name", "platform", "size", "sha256"]
LIVE_TARGET_KEYS = ["platform", "name", "size", "sha256"]
RELEASE_MANIFEST_KEYS = [
    "schema_version", "plan_id", "revision", "version", "candidate_id",
    "source_commit", "source_tree", "build_input_sha256",
    "release_sha256sums_sha256", "assets",
]

RELEASE_PAYLOADS = {
    "install.ps1": "windows",
    "install.sh": "unix",
    "ipgw-meta-darwin-amd64.tar.gz": "darwin-amd64",
    "ipgw-meta-darwin-arm64.tar.gz": "darwin-arm64",
    "ipgw-meta-linux-amd64.tar.gz": "linux-amd64",
    "ipgw-meta-linux-arm64.tar.gz": "linux-arm64",
    "ipgw-meta-windows-amd64.zip": "windows-amd64",
    "ipgw-meta-windows-arm64.zip": "windows-arm64",
}
PUBLIC_ASSETS = {
    **{f"release/{name}": platform for name, platform in RELEASE_PAYLOADS.items()},
    "release/SHA256SUMS": "all",
    "release/release-manifest.json": "all",
}
TEST_TOOLS = {
    "test-tools/ipgw-live-gate-linux-amd64": "linux-amd64",
    "test-tools/ipgw-live-gate-windows-amd64.exe": "windows-amd64",
}
ROOT_FILES = {"candidate-manifest.json", "SHA256SUMS", *PUBLIC_ASSETS, *TEST_TOOLS}
PUBLIC_BASENAMES = {PurePosixPath(name).name for name in PUBLIC_ASSETS}
REQUIRED_EVIDENCE_TUPLES = {
    ("linux-amd64", "nas_vm", "campus_wired", "password", "password_core"),
    ("linux-amd64", "nas_vm", "campus_wired", "terminal_qr", "terminal_qr"),
    ("windows-amd64", "bhk_windows", "campus_wired", "password", "password_core"),
    ("windows-amd64", "bhk_windows", "campus_wifi", "password", "password_core"),
}
PASSWORD_BEFORE = ["synthetic_covered", "live_unverified"]
PASSWORD_AFTER = ["synthetic_covered", "live_verified"]
QR_BEFORE = ["observed_anonymous", "synthetic_covered", "live_unverified"]
QR_AFTER = ["observed_anonymous", "synthetic_covered", "live_verified"]
RELEASE_NOTES = """# IPGW-Meta v1.0.0

- 新安装默认模式：`meta`。
- 既有安装默认模式：保持 `legacy`；迁移必须由维护者显式执行。
- 配置迁移：使用 v1 事务化 preview/apply 流程；失败保持旧配置并保留可恢复状态。
- Password 真实证据：promotion lock 绑定 NAS campus wired、BHK campus wired 与 BHK campus Wi-Fi 三项通过记录。
- Terminal QR 真实证据：promotion lock 绑定 NAS campus wired 一项真实闭环通过记录。
- 异账号 conflict/switch：仅有合成覆盖，不声明真实双账号验收。
- OTP：仅 `observed_anonymous + detected_only`，不声明登录支持。
- 自更新：禁用；请从官方 GitHub Release 获取并验证资产。
- macOS：仅完成原生安装和 CLI smoke，没有校园网认证证据。
""".encode("utf-8")


class VerificationError(Exception):
    pass


def reject(reason: str = "invalid") -> None:
    raise VerificationError(reason)


def is_positive_int(value: object) -> bool:
    return type(value) is int and value > 0


def is_hex40(value: object) -> bool:
    return isinstance(value, str) and HEX40.fullmatch(value) is not None


def is_hex64(value: object) -> bool:
    return isinstance(value, str) and HEX64.fullmatch(value) is not None


def exact_keys(value: object, keys: list[str]) -> dict:
    if not isinstance(value, dict) or list(value.keys()) != keys:
        reject("keys")
    return value


def no_duplicate_object(pairs: list[tuple[str, object]]) -> dict:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            reject("duplicate")
        result[key] = value
    return result


def reject_constant(_: str) -> None:
    reject("constant")


def canonical_bytes(value: object) -> bytes:
    try:
        return json.dumps(
            value, ensure_ascii=False, allow_nan=False, separators=(",", ":")
        ).encode("utf-8") + b"\n"
    except (TypeError, ValueError, UnicodeError) as exc:
        raise VerificationError("json") from exc


def read_regular(path: Path, maximum: int) -> bytes:
    try:
        info = path.lstat()
    except OSError as exc:
        raise VerificationError("file") from exc
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size < 1 or info.st_size > maximum:
        reject("file")
    try:
        with path.open("rb") as stream:
            data = stream.read(maximum + 1)
    except OSError as exc:
        raise VerificationError("file") from exc
    if len(data) != info.st_size or len(data) > maximum:
        reject("file")
    return data


def decode_json(raw: bytes, *, canonical: bool) -> object:
    try:
        text = raw.decode("utf-8", errors="strict")
        value = json.loads(text, object_pairs_hook=no_duplicate_object, parse_constant=reject_constant)
    except (UnicodeError, json.JSONDecodeError, VerificationError) as exc:
        raise VerificationError("json") from exc
    if canonical and raw != canonical_bytes(value):
        reject("canonical")
    return value


def load_canonical(path: Path, maximum: int = MAX_JSON_BYTES) -> object:
    return decode_json(read_regular(path, maximum), canonical=True)


def load_untrusted_json(path: Path, maximum: int = 4 * 1024 * 1024) -> object:
    return decode_json(read_regular(path, maximum), canonical=False)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path, maximum: int = MAX_ARTIFACT_BYTES) -> tuple[int, str]:
    try:
        info = path.lstat()
    except OSError as exc:
        raise VerificationError("file") from exc
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size < 1 or info.st_size > maximum:
        reject("file")
    digest = hashlib.sha256()
    size = 0
    try:
        with path.open("rb") as stream:
            while True:
                chunk = stream.read(1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                if size > maximum:
                    reject("file")
                digest.update(chunk)
    except OSError as exc:
        raise VerificationError("file") from exc
    if size != info.st_size:
        reject("file")
    return size, digest.hexdigest()


def valid_ascii_name(name: object, *, basename: bool = False) -> bool:
    if not isinstance(name, str) or not name or "\\" in name:
        return False
    try:
        raw = name.encode("ascii")
    except UnicodeError:
        return False
    if any(byte < 0x20 or byte > 0x7E or byte == 0x7F for byte in raw):
        return False
    path = PurePosixPath(name)
    if path.is_absolute() or str(path) != name or any(part in ("", ".", "..") for part in path.parts):
        return False
    return not basename or len(path.parts) == 1


def validate_candidate_id(value: object, source: str, run_id: int, attempt: int) -> str:
    if not isinstance(value, str):
        reject("candidate")
    match = CANDIDATE_ID.fullmatch(value)
    if match is None or match.group(1) != source[:12] or int(match.group(2)) != run_id or int(match.group(3)) != attempt:
        reject("candidate")
    return value


def validate_timestamp(value: object) -> dt.datetime:
    if not isinstance(value, str) or RFC3339_UTC.fullmatch(value) is None:
        reject("time")
    try:
        parsed = dt.datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError as exc:
        raise VerificationError("time") from exc
    if parsed.tzinfo != dt.timezone.utc:
        reject("time")
    return parsed


def validate_lock_shape(lock: object) -> dict:
    lock = exact_keys(lock, LOCK_KEYS)
    if lock["schema_version"] != 1 or lock["plan_id"] != PLAN_ID or lock["revision"] != REVISION or lock["version"] != VERSION:
        reject("identity")
    if not is_hex40(lock["source_commit"]) or not is_hex40(lock["source_tree"]):
        reject("source")
    workflow = exact_keys(lock["workflow"], WORKFLOW_KEYS)
    if workflow["repository_id"] != REPOSITORY_ID or workflow["path"] != CANDIDATE_WORKFLOW or not is_positive_int(workflow["run_id"]) or not is_positive_int(workflow["run_attempt"]):
        reject("workflow")
    candidate_id = validate_candidate_id(lock["candidate_id"], lock["source_commit"], workflow["run_id"], workflow["run_attempt"])
    artifact = exact_keys(lock["artifact"], ARTIFACT_KEYS)
    if not is_positive_int(artifact["id"]) or artifact["name"] != f"candidate-set-{candidate_id}" or not isinstance(artifact["digest"], str) or re.fullmatch(r"sha256:[0-9a-f]{64}", artifact["digest"]) is None:
        reject("artifact")
    for key in ("candidate_set_sha256", "release_manifest_sha256", "build_input_sha256", "release_notes_sha256"):
        if not is_hex64(lock[key]):
            reject("digest")
    subjects = lock["attestation_subjects"]
    if not isinstance(subjects, list) or len(subjects) != 11:
        reject("subjects")
    names: list[str] = []
    for subject in subjects:
        subject = exact_keys(subject, SUBJECT_KEYS)
        if not valid_ascii_name(subject["name"], basename=True) or not is_hex64(subject["sha256"]):
            reject("subjects")
        names.append(subject["name"])
    expected_names = sorted({artifact["name"], *PUBLIC_BASENAMES})
    if names != sorted(names) or len({name.lower() for name in names}) != 11 or names != expected_names:
        reject("subjects")
    subject_map = {item["name"]: item["sha256"] for item in subjects}
    if subject_map[artifact["name"]] != artifact["digest"].removeprefix("sha256:"):
        reject("subjects")
    evidence_ids = lock["evidence_ids"]
    if not isinstance(evidence_ids, list) or len(evidence_ids) != 4 or evidence_ids != sorted(evidence_ids) or len(set(evidence_ids)) != 4:
        reject("evidence")
    for evidence_id in evidence_ids:
        if not isinstance(evidence_id, str) or EVIDENCE_ID.fullmatch(evidence_id) is None or evidence_id.endswith("-000"):
            reject("evidence")
    return lock


def git_env() -> dict[str, str]:
    environment = os.environ.copy()
    environment["LC_ALL"] = "C"
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    return environment


def git_scalar(root: Path, *args: str, maximum: int = 4096) -> bytes:
    try:
        completed = subprocess.run(
            ["git", *args], cwd=root, env=git_env(), stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False,
        )
    except OSError as exc:
        raise VerificationError("git") from exc
    if completed.returncode != 0 or len(completed.stdout) > maximum:
        reject("git")
    return completed.stdout


def git_success(root: Path, *args: str) -> bool:
    try:
        return subprocess.run(
            ["git", *args], cwd=root, env=git_env(), stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False,
        ).returncode == 0
    except OSError as exc:
        raise VerificationError("git") from exc


def promotion_whitelist(path: str) -> bool:
    return path in {"docs/upgrade/status.md", "docs/compatibility/auth-capabilities.md"} or path.startswith("docs/evidence/releases/v1.0.0/")


def nul_records(stream: BinaryIO, maximum: int) -> Iterable[bytes]:
    buffer = bytearray()
    while True:
        chunk = stream.read(8192)
        if not chunk:
            if buffer:
                reject("git")
            return
        buffer.extend(chunk)
        if len(buffer) > maximum and 0 not in buffer:
            reject("git")
        while True:
            index = buffer.find(0)
            if index < 0:
                break
            record = bytes(buffer[:index])
            del buffer[: index + 1]
            if not record or len(record) > maximum:
                reject("git")
            yield record


def read_exact(stream: BinaryIO, count: int, digest: object) -> int:
    remaining = count
    while remaining:
        chunk = stream.read(min(1024 * 1024, remaining))
        if not chunk:
            reject("git")
        digest.update(chunk)
        remaining -= len(chunk)
    return count


def finish_process(process: subprocess.Popen) -> int:
    try:
        return process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        try:
            return process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            reject("git")


def hash_build_input(root: Path, commit: str) -> str:
    if not is_hex40(commit):
        reject("git")
    listing: subprocess.Popen | None = None
    blobs: subprocess.Popen | None = None
    try:
        listing = subprocess.Popen(
            ["git", "ls-tree", "-rz", "--full-tree", "-r", commit], cwd=root,
            env=git_env(), stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )
        blobs = subprocess.Popen(
            ["git", "cat-file", "--batch"], cwd=root, env=git_env(),
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
        )
    except OSError as exc:
        if listing is not None:
            listing.kill()
            finish_process(listing)
        raise VerificationError("git") from exc
    if listing is None or blobs is None or listing.stdout is None or blobs.stdin is None or blobs.stdout is None:
        reject("git")
    result = hashlib.sha256()
    previous: bytes | None = None
    seen = False
    try:
        for record in nul_records(listing.stdout, MAX_PATH_BYTES + 128):
            tab = record.find(b"\t")
            metadata = record[:tab].split() if tab >= 0 else []
            path = record[tab + 1 :] if tab >= 0 else b""
            try:
                oid_text = metadata[2].decode("ascii", "strict") if len(metadata) == 3 else ""
            except UnicodeError as exc:
                raise VerificationError("git") from exc
            if len(metadata) != 3 or metadata[0] not in (b"100644", b"100755") or metadata[1] != b"blob" or HEX40.fullmatch(oid_text) is None or not path or len(path) > MAX_PATH_BYTES:
                reject("git")
            try:
                path_text = path.decode("utf-8", "strict")
            except UnicodeError as exc:
                raise VerificationError("git") from exc
            if previous is not None and previous >= path:
                reject("git")
            previous = path
            seen = True
            if promotion_whitelist(path_text):
                continue
            oid = metadata[2]
            blobs.stdin.write(oid + b"\n")
            blobs.stdin.flush()
            header = blobs.stdout.readline(256)
            fields = header.rstrip(b"\n").split()
            if len(fields) != 3 or fields[0] != oid or fields[1] != b"blob":
                reject("git")
            try:
                size = int(fields[2].decode("ascii"))
            except (ValueError, UnicodeError) as exc:
                raise VerificationError("git") from exc
            if size < 0 or str(size).encode("ascii") != fields[2]:
                reject("git")
            digest = hashlib.sha256()
            read_exact(blobs.stdout, size, digest)
            if blobs.stdout.read(1) != b"\n":
                reject("git")
            result.update(path)
            result.update(b"\0")
            result.update(metadata[0])
            result.update(b"\0")
            result.update(str(size).encode("ascii"))
            result.update(b"\0")
            result.update(digest.hexdigest().encode("ascii"))
            result.update(b"\n")
    finally:
        try:
            listing.stdout.close()
        except OSError:
            pass
        try:
            blobs.stdin.close()
        except OSError:
            pass
        try:
            blobs.stdout.close()
        except OSError:
            pass
        listing_code = finish_process(listing)
        blobs_code = finish_process(blobs)
    if listing_code != 0 or blobs_code != 0 or not seen:
        reject("git")
    return result.hexdigest()


def validate_source(root: Path, lock: dict, tag_commit: str) -> None:
    if not is_hex40(tag_commit):
        reject("tag")
    resolved = git_scalar(root, "rev-parse", "--verify", f"{tag_commit}^{{commit}}").strip().decode("ascii")
    if resolved != tag_commit or not git_success(root, "merge-base", "--is-ancestor", lock["source_commit"], tag_commit):
        reject("source")
    source_tree = git_scalar(root, "rev-parse", "--verify", f"{lock['source_commit']}^{{tree}}").strip().decode("ascii")
    if source_tree != lock["source_tree"]:
        reject("source")
    try:
        changed = subprocess.run(
            ["git", "diff", "--no-renames", "--name-only", "-z", lock["source_commit"], tag_commit, "--"],
            cwd=root, env=git_env(), stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, check=False,
        )
    except OSError as exc:
        raise VerificationError("git") from exc
    if changed.returncode != 0 or len(changed.stdout) > 4 * 1024 * 1024:
        reject("git")
    names = changed.stdout.split(b"\0")
    if not names or names[-1] != b"":
        reject("git")
    for raw in names[:-1]:
        try:
            name = raw.decode("utf-8", "strict")
        except UnicodeError as exc:
            raise VerificationError("git") from exc
        if not raw or len(raw) > MAX_PATH_BYTES or not promotion_whitelist(name):
            reject("whitelist")
    if hash_build_input(root, tag_commit) != lock["build_input_sha256"]:
        reject("build-input")


def validate_evidence(directory: Path, lock: dict) -> None:
    tuples: set[tuple[str, str, str, str, str]] = set()
    for evidence_id in lock["evidence_ids"]:
        summary = exact_keys(load_canonical(directory / f"{evidence_id}.json"), EVIDENCE_KEYS)
        if summary["schema_version"] != 1 or summary["plan_id"] != PLAN_ID or summary["revision"] != REVISION or summary["evidence_id"] != evidence_id:
            reject("evidence")
        if summary["candidate_id"] != lock["candidate_id"] or summary["candidate_set_sha256"] != lock["candidate_set_sha256"] or summary["source_commit"] != lock["source_commit"] or not is_hex64(summary["bundle_sha256"]):
            reject("evidence")
        if summary["result"] != "pass":
            reject("evidence")
        value = (
            summary["platform"], summary["testbed"], summary["network_type"],
            summary["auth_method"], summary["suite"],
        )
        if value not in REQUIRED_EVIDENCE_TUPLES or value in tuples:
            reject("evidence")
        tuples.add(value)
        before, after = (PASSWORD_BEFORE, PASSWORD_AFTER) if summary["suite"] == "password_core" else (QR_BEFORE, QR_AFTER)
        if summary["capability_before"] != before or summary["capability_after"] != after:
            reject("evidence")
        started = validate_timestamp(summary["started_at"])
        finished = validate_timestamp(summary["finished_at"])
        match = EVIDENCE_ID.fullmatch(evidence_id)
        assert match is not None
        try:
            evidence_date = dt.date(int(match.group(1)), int(match.group(2)), int(match.group(3)))
        except ValueError as exc:
            raise VerificationError("evidence") from exc
        if evidence_date != started.date() or finished < started:
            reject("evidence")
    if tuples != REQUIRED_EVIDENCE_TUPLES:
        reject("evidence")


def validate_lock(repository_root: Path, lock_path: Path, notes_path: Path, tag_commit: str) -> dict:
    repository_root = repository_root.resolve(strict=True)
    expected_directory = repository_root / "docs" / "evidence" / "releases" / VERSION
    if lock_path.resolve(strict=True) != expected_directory / "promotion-lock.json" or notes_path.resolve(strict=True) != expected_directory / "release-notes.md":
        reject("path")
    lock = validate_lock_shape(load_canonical(lock_path))
    notes = read_regular(notes_path, MAX_JSON_BYTES)
    if notes != RELEASE_NOTES or sha256_bytes(notes) != lock["release_notes_sha256"]:
        reject("notes")
    validate_source(repository_root, lock, tag_commit)
    validate_evidence(expected_directory, lock)
    return lock


def validate_assets(values: object, expected: dict[str, str]) -> dict[str, dict]:
    if not isinstance(values, list) or len(values) != len(expected):
        reject("assets")
    result: dict[str, dict] = {}
    names: list[str] = []
    for item in values:
        item = exact_keys(item, ASSET_KEYS)
        name = item["name"]
        if name not in expected or item["platform"] != expected[name] or not is_positive_int(item["size"]) or not is_hex64(item["sha256"]) or not valid_ascii_name(name):
            reject("assets")
        names.append(name)
        result[name] = item
    if names != sorted(expected) or len({name.lower() for name in names}) != len(names):
        reject("assets")
    return result


def parse_checksums(path: Path, expected_names: set[str]) -> dict[str, str]:
    raw = read_regular(path, MAX_JSON_BYTES)
    try:
        text = raw.decode("ascii", "strict")
    except UnicodeError as exc:
        raise VerificationError("checksums") from exc
    if not text.endswith("\n") or "\r" in text:
        reject("checksums")
    result: dict[str, str] = {}
    names: list[str] = []
    for line in text[:-1].split("\n"):
        match = re.fullmatch(r"([0-9a-f]{64})  ([0-9A-Za-z._/-]+)", line)
        if match is None or not valid_ascii_name(match.group(2)) or match.group(2) in result:
            reject("checksums")
        result[match.group(2)] = match.group(1)
        names.append(match.group(2))
    if set(names) != expected_names or names != sorted(names):
        reject("checksums")
    return result


def exact_tree(root: Path, expected_directories: set[str]) -> set[str]:
    result: set[str] = set()
    found_directories: set[str] = set()
    try:
        for base, directories, files in os.walk(root, topdown=True, followlinks=False):
            base_path = Path(base)
            for name in [*directories, *files]:
                path = base_path / name
                info = path.lstat()
                relative = path.relative_to(root).as_posix()
                if stat.S_ISLNK(info.st_mode):
                    reject("tree")
                if stat.S_ISREG(info.st_mode):
                    if info.st_nlink != 1:
                        reject("tree")
                    result.add(relative)
                elif stat.S_ISDIR(info.st_mode):
                    found_directories.add(relative)
                else:
                    reject("tree")
    except OSError as exc:
        raise VerificationError("tree") from exc
    if found_directories != expected_directories:
        reject("tree")
    return result


def validate_candidate_root(root: Path, lock: dict, repository_root: Path) -> dict[str, dict]:
    root = root.resolve(strict=True)
    if exact_tree(root, {"release", "test-tools"}) != ROOT_FILES:
        reject("tree")
    manifest_raw = read_regular(root / "candidate-manifest.json", MAX_JSON_BYTES)
    manifest = exact_keys(decode_json(manifest_raw, canonical=True), MANIFEST_KEYS)
    workflow = lock["workflow"]
    if manifest["schema_version"] != 1 or manifest["plan_id"] != PLAN_ID or manifest["revision"] != REVISION or manifest["version"] != VERSION or manifest["candidate_id"] != lock["candidate_id"] or manifest["source_commit"] != lock["source_commit"] or manifest["source_tree"] != lock["source_tree"] or manifest["workflow_run_id"] != workflow["run_id"] or manifest["workflow_run_attempt"] != workflow["run_attempt"] or manifest["build_input_sha256"] != lock["build_input_sha256"]:
        reject("manifest")
    if sha256_bytes(manifest_raw) != lock["candidate_set_sha256"]:
        reject("manifest")
    toolchain = exact_keys(manifest["toolchain"], TOOLCHAIN_KEYS)
    if toolchain.get("go_version") != "go1.25.0" or toolchain.get("go_toolchain") != "local" or toolchain.get("host_platform") != "linux-amd64" or toolchain.get("cgo_enabled") is not False or toolchain.get("goamd64") != "v1" or toolchain.get("goarm64") != "v8.0" or not is_positive_int(toolchain.get("source_date_epoch")) or toolchain.get("build_recipe") != "candidate-v1":
        reject("toolchain")
    repository_root = repository_root.resolve(strict=True)
    epoch_raw = git_scalar(repository_root, "show", "-s", "--format=%ct", lock["source_commit"]).strip()
    if epoch_raw != str(toolchain["source_date_epoch"]).encode("ascii"):
        reject("toolchain")
    release_assets = validate_assets(manifest["release_assets"], PUBLIC_ASSETS)
    test_tools = validate_assets(manifest["test_tools"], TEST_TOOLS)
    live_targets = manifest["live_gate_targets"]
    if not isinstance(live_targets, list) or len(live_targets) != 2:
        reject("targets")
    expected_targets = [("linux-amd64", "ipgw-meta"), ("windows-amd64", "ipgw-meta.exe")]
    for item, expected in zip(live_targets, expected_targets, strict=True):
        item = exact_keys(item, LIVE_TARGET_KEYS)
        if (item["platform"], item["name"]) != expected or not is_positive_int(item["size"]) or item["size"] > 64 * 1024 * 1024 or not is_hex64(item["sha256"]):
            reject("targets")
    all_assets = {**release_assets, **test_tools}
    for name, item in all_assets.items():
        size, digest = sha256_file(root / name, candidate_member_limit(name))
        if size != item["size"] or digest != item["sha256"]:
            reject("asset")
    for name, item in test_tools.items():
        content = read_regular(root / name, MAX_BINARY_BYTES)
        validate_go_binary(content, item["platform"], "ipgw-live-gate", True)
    bundle_summaries: dict[str, dict[str, dict[str, object]]] = {}
    for target in TARGETS:
        extension = ".zip" if target.startswith("windows-") else ".tar.gz"
        bundle_name = f"ipgw-meta-{target}{extension}"
        bundle_summaries[target] = validate_bundle(root / "release" / bundle_name, target, toolchain["source_date_epoch"])
    for index, (target, name) in enumerate(expected_targets):
        metric = bundle_summaries[target][name]
        item = live_targets[index]
        if item["size"] != metric["size"] or item["sha256"] != metric["sha256"]:
            reject("targets")
    release_manifest_raw = read_regular(root / "release" / "release-manifest.json", MAX_JSON_BYTES)
    release_manifest = exact_keys(decode_json(release_manifest_raw, canonical=True), RELEASE_MANIFEST_KEYS)
    if sha256_bytes(release_manifest_raw) != lock["release_manifest_sha256"] or release_manifest["schema_version"] != 1 or release_manifest["plan_id"] != PLAN_ID or release_manifest["revision"] != REVISION or release_manifest["version"] != VERSION or release_manifest["candidate_id"] != lock["candidate_id"] or release_manifest["source_commit"] != lock["source_commit"] or release_manifest["source_tree"] != lock["source_tree"] or release_manifest["build_input_sha256"] != lock["build_input_sha256"]:
        reject("release-manifest")
    payload_assets = validate_assets(release_manifest["assets"], RELEASE_PAYLOADS)
    release_checksums = parse_checksums(root / "release" / "SHA256SUMS", set(RELEASE_PAYLOADS))
    checksum_raw = read_regular(root / "release" / "SHA256SUMS", MAX_JSON_BYTES)
    if sha256_bytes(checksum_raw) != release_manifest["release_sha256sums_sha256"]:
        reject("checksums")
    for name, item in payload_assets.items():
        release_item = release_assets[f"release/{name}"]
        expected_item = {
            "name": name, "platform": release_item["platform"],
            "size": release_item["size"], "sha256": release_item["sha256"],
        }
        if item != expected_item or release_checksums[name] != item["sha256"]:
            reject("release-manifest")
    root_checksums = parse_checksums(root / "SHA256SUMS", ROOT_FILES - {"SHA256SUMS"})
    for name in ROOT_FILES - {"SHA256SUMS"}:
        _, digest = sha256_file(root / name, candidate_member_limit(name))
        if root_checksums[name] != digest:
            reject("checksums")
    subject_map = {item["name"]: item["sha256"] for item in lock["attestation_subjects"]}
    for name, item in release_assets.items():
        if subject_map[PurePosixPath(name).name] != item["sha256"]:
            reject("subjects")
    return release_assets


def extract_artifact(archive: Path, destination: Path, lock: dict, repository_root: Path) -> dict[str, dict]:
    _, digest = sha256_file(archive)
    if digest != lock["artifact"]["digest"].removeprefix("sha256:"):
        reject("artifact")
    if destination.exists() or not destination.parent.is_dir():
        reject("destination")
    destination.mkdir(mode=0o700)
    seen: set[str] = set()
    total = 0
    seen_directories: set[str] = set()
    try:
        with zipfile.ZipFile(archive, "r") as bundle:
            for entry in bundle.infolist():
                name = entry.filename
                if entry.flag_bits & 0x1 or "\\" in name or name.startswith("/"):
                    reject("zip")
                mode = (entry.external_attr >> 16) & 0o170000
                if name.endswith("/"):
                    if name not in {"release/", "test-tools/"} or name in seen_directories or mode not in (0, stat.S_IFDIR):
                        reject("zip")
                    seen_directories.add(name)
                    continue
                if name not in ROOT_FILES or name in seen or not valid_ascii_name(name):
                    reject("zip")
                if mode not in (0, stat.S_IFREG):
                    reject("zip")
                maximum = candidate_member_limit(name)
                total += entry.file_size
                if entry.file_size < 1 or entry.file_size > maximum or total > MAX_ARTIFACT_BYTES:
                    reject("zip")
                target = destination / PurePosixPath(name)
                target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with bundle.open(entry, "r") as source, target.open("xb") as output:
                    copied = 0
                    while True:
                        chunk = source.read(1024 * 1024)
                        if not chunk:
                            break
                        copied += len(chunk)
                        if copied > entry.file_size:
                            reject("zip")
                        output.write(chunk)
                if copied != entry.file_size:
                    reject("zip")
                seen.add(name)
    except (OSError, zipfile.BadZipFile, RuntimeError) as exc:
        raise VerificationError("zip") from exc
    if seen != ROOT_FILES:
        reject("zip")
    return validate_candidate_root(destination, lock, repository_root)


def validate_candidate_api(lock: dict, run_path: Path, artifact_path: Path) -> None:
    run = load_untrusted_json(run_path)
    artifact = load_untrusted_json(artifact_path)
    workflow = lock["workflow"]
    if (
        not isinstance(run, dict)
        or run.get("id") != workflow["run_id"]
        or run.get("run_attempt") != workflow["run_attempt"]
        or run.get("path") != CANDIDATE_WORKFLOW
        or run.get("event") != "workflow_dispatch"
        or run.get("head_branch") != "main"
        or run.get("head_sha") != lock["source_commit"]
        or run.get("status") != "completed"
        or run.get("conclusion") != "success"
        or not isinstance(run.get("repository"), dict)
        or run["repository"].get("id") != REPOSITORY_ID
        or not isinstance(run.get("head_repository"), dict)
        or run["head_repository"].get("id") != REPOSITORY_ID
    ):
        reject("run")
    expected = lock["artifact"]
    workflow_run = artifact.get("workflow_run") if isinstance(artifact, dict) else None
    if (
        not isinstance(artifact, dict)
        or artifact.get("id") != expected["id"]
        or artifact.get("name") != expected["name"]
        or artifact.get("digest") != expected["digest"]
        or artifact.get("expired") is not False
        or not is_positive_int(artifact.get("size_in_bytes"))
        or not isinstance(workflow_run, dict)
        or workflow_run.get("id") != workflow["run_id"]
        or workflow_run.get("repository_id") != REPOSITORY_ID
        or workflow_run.get("head_repository_id") != REPOSITORY_ID
        or workflow_run.get("head_branch") != "main"
        or workflow_run.get("head_sha") != lock["source_commit"]
    ):
        reject("artifact-api")


def attestation_subjects(result: object, invocation: str) -> list[dict]:
    if not isinstance(result, list) or len(result) != 1 or not isinstance(result[0], dict):
        reject("attestation")
    verification = result[0].get("verificationResult")
    if not isinstance(verification, dict):
        reject("attestation")
    signature = verification.get("signature")
    certificate = signature.get("certificate") if isinstance(signature, dict) else None
    statement = verification.get("statement")
    if not isinstance(certificate, dict) or certificate.get("runInvocationURI") != invocation or not isinstance(statement, dict) or statement.get("predicateType") != "https://slsa.dev/provenance/v1" or not isinstance(statement.get("subject"), list):
        reject("attestation")
    return statement["subject"]


def normalized_statement_subjects(values: object) -> list[dict]:
    if not isinstance(values, list):
        reject("attestation")
    result: list[dict] = []
    for item in values:
        if not isinstance(item, dict) or set(item) != {"name", "digest"} or not valid_ascii_name(item["name"], basename=True) or not isinstance(item["digest"], dict) or set(item["digest"]) != {"sha256"} or not is_hex64(item["digest"]["sha256"]):
            reject("attestation")
        result.append({"name": item["name"], "sha256": item["digest"]["sha256"]})
    return sorted(result, key=lambda item: item["name"])


def validate_attestations(lock: dict, candidate_path: Path, public_directory: Path) -> None:
    workflow = lock["workflow"]
    invocation = f"https://github.com/UnbalancedCat/ipgw-meta/actions/runs/{workflow['run_id']}/attempts/{workflow['run_attempt']}"
    candidate_expected = [{
        "name": lock["artifact"]["name"],
        "sha256": lock["artifact"]["digest"].removeprefix("sha256:"),
    }]
    candidate_values = attestation_subjects(load_untrusted_json(candidate_path), invocation)
    if normalized_statement_subjects(candidate_values) != candidate_expected:
        reject("attestation")
    public_expected = [
        item for item in lock["attestation_subjects"]
        if item["name"] != lock["artifact"]["name"]
    ]
    if not public_directory.is_dir():
        reject("attestation")
    expected_files = {f"{name}.json" for name in PUBLIC_BASENAMES}
    if set(os.listdir(public_directory)) != expected_files:
        reject("attestation")
    for name in sorted(PUBLIC_BASENAMES):
        values = attestation_subjects(
            load_untrusted_json(public_directory / f"{name}.json"), invocation
        )
        if normalized_statement_subjects(values) != public_expected:
            reject("attestation")

def bytes_metrics(data: bytes) -> dict[str, object]:
    return {"size": len(data), "sha256": sha256_bytes(data)}


def checksum_bytes(values: dict[str, dict[str, object]]) -> bytes:
    names = sorted(values)
    if not names or any(not valid_ascii_name(name) for name in names):
        reject("checksums")
    return "".join(f"{values[name]['sha256']}  {name}\n" for name in names).encode("ascii")


def bundle_binary_names(target: str) -> list[str]:
    if target not in TARGETS:
        reject("bundle")
    suffix = ".exe" if target.startswith("windows-") else ""
    return [f"{name}{suffix}" for name in ("ipgw", "ipgw-meta", "ipgw-legacy")]


def bundle_manifest_bytes(target: str, contents: dict[str, bytes]) -> bytes:
    entries = bundle_binary_names(target)
    metrics = [bytes_metrics(contents[name]) for name in entries]
    return (
        "{\n"
        '  "schema_version": 1,\n'
        '  "product": "ipgw-meta",\n'
        '  "module": "github.com/UnbalancedCat/ipgw-meta",\n'
        '  "version": "v1.0.0",\n'
        f'  "platform": "{target}",\n'
        '  "entries": [\n'
        f'    {{"path": "{entries[0]}", "sha256": "{metrics[0]["sha256"]}", "size": {metrics[0]["size"]}}},\n'
        f'    {{"path": "{entries[1]}", "sha256": "{metrics[1]["sha256"]}", "size": {metrics[1]["size"]}}},\n'
        f'    {{"path": "{entries[2]}", "sha256": "{metrics[2]["sha256"]}", "size": {metrics[2]["size"]}}}\n'
        "  ],\n"
        '  "launcher_default": "meta",\n'
        '  "layout": "versioned-bundle-v1",\n'
        '  "self_update": false,\n'
        '  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}\n'
        "}\n"
    ).encode("utf-8")


def bundle_member_limit(name: str, target: str) -> tuple[int, int]:
    if name in bundle_binary_names(target):
        return 0o755, MAX_BINARY_BYTES
    if name == "LICENSE":
        return 0o644, MAX_INSTALLER_BYTES
    if name in {"launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"}:
        return 0o644, MAX_JSON_BYTES
    reject("bundle")


def validate_bundle_contents(contents: dict[str, bytes], target: str) -> dict[str, dict[str, object]]:
    expected = {*bundle_binary_names(target), *BUNDLE_STATIC_FILES}
    if set(contents) != expected or len(contents) != 7:
        reject("bundle")
    if contents["launcher-default.yaml"] != b"schema_version: 1\nmode: meta\ncohort: new-install\n":
        reject("bundle")
    if contents["bundle-manifest.json"] != bundle_manifest_bytes(target, contents):
        reject("bundle")
    metrics = {
        name: bytes_metrics(value)
        for name, value in contents.items()
        if name != "SHA256SUMS"
    }
    if contents["SHA256SUMS"] != checksum_bytes(metrics):
        reject("bundle")
    return metrics


def decode_single_gzip(raw: bytes, epoch: int) -> bytes:
    if len(raw) < 18 or epoch < 1 or epoch > 0xFFFFFFFF:
        reject("gzip")
    identifier1, identifier2, method, flags, modified, extra_flags, operating_system = struct.unpack(
        "<BBBBIBB", raw[:10]
    )
    if (identifier1, identifier2, method, flags) != (0x1F, 0x8B, 8, 0) or modified != epoch or extra_flags != 2 or operating_system != 255:
        reject("gzip")
    maximum = 3 * MAX_BINARY_BYTES + 2 * MAX_INSTALLER_BYTES + 3 * MAX_JSON_BYTES
    try:
        decompressor = zlib.decompressobj(-zlib.MAX_WBITS)
        content = decompressor.decompress(raw[10:], maximum + 1)
    except zlib.error as exc:
        raise VerificationError("gzip") from exc
    if len(content) > maximum or not decompressor.eof or decompressor.unconsumed_tail or len(decompressor.unused_data) != 8:
        reject("gzip")
    checksum, size = struct.unpack("<II", decompressor.unused_data)
    if checksum != zlib.crc32(content) or size != (len(content) & 0xFFFFFFFF):
        reject("gzip")
    return content


def tar_text(field: bytes) -> str:
    separator = field.find(b"\0")
    if separator < 0:
        separator = len(field)
    elif any(field[separator:]):
        reject("tar")
    try:
        return field[:separator].decode("ascii", "strict")
    except UnicodeError as exc:
        raise VerificationError("tar") from exc


def tar_octal(field: bytes) -> int:
    if field and field[0] & 0x80:
        reject("tar")
    value = field.strip(b" \0")
    if not value or any(character < ord("0") or character > ord("7") for character in value):
        reject("tar")
    return int(value, 8)


def parse_tar_bundle(raw: bytes, target: str, epoch: int) -> dict[str, bytes]:
    data = decode_single_gzip(raw, epoch)
    expected_names = sorted({*bundle_binary_names(target), *BUNDLE_STATIC_FILES})
    contents: dict[str, bytes] = {}
    offset = 0
    for expected_name in expected_names:
        if offset + 512 > len(data):
            reject("tar")
        header = data[offset : offset + 512]
        offset += 512
        stored_checksum = tar_octal(header[148:156])
        calculated_checksum = sum(header[:148]) + 8 * ord(" ") + sum(header[156:])
        name = tar_text(header[0:100])
        mode = tar_octal(header[100:108])
        size = tar_octal(header[124:136])
        expected_mode, maximum = bundle_member_limit(name, target)
        if (
            stored_checksum != calculated_checksum
            or name != expected_name
            or mode != expected_mode
            or tar_octal(header[108:116]) != 0
            or tar_octal(header[116:124]) != 0
            or tar_octal(header[136:148]) != epoch
            or header[156:157] != b"0"
            or tar_text(header[157:257]) != ""
            or header[257:263] != b"ustar\0"
            or header[263:265] != b"00"
            or tar_text(header[265:297]) != ""
            or tar_text(header[297:329]) != ""
            or tar_octal(header[329:337]) != 0
            or tar_octal(header[337:345]) != 0
            or tar_text(header[345:500]) != ""
            or any(header[500:512])
            or size < 1
            or size > maximum
            or offset + size > len(data)
        ):
            reject("tar")
        contents[name] = data[offset : offset + size]
        offset += size
        padding = (-size) % 512
        if offset + padding > len(data) or any(data[offset : offset + padding]):
            reject("tar")
        offset += padding
    if data[offset:] != bytes(1024):
        reject("tar")
    return contents

def zip_dos_time(epoch: int) -> tuple[int, int, tuple[int, int, int, int, int, int]]:
    try:
        value = dt.datetime.fromtimestamp(epoch, tz=dt.timezone.utc)
    except (OverflowError, OSError, ValueError) as exc:
        raise VerificationError("zip") from exc
    if value.year < 1980 or value.year > 2107:
        reject("zip")
    date = ((value.year - 1980) << 9) | (value.month << 5) | value.day
    clock = (value.hour << 11) | (value.minute << 5) | (value.second // 2)
    stamp = (value.year, value.month, value.day, value.hour, value.minute, value.second // 2 * 2)
    return date, clock, stamp


def parse_zip_bundle(raw: bytes, target: str, epoch: int) -> dict[str, bytes]:
    if len(raw) < 22 or raw[-22:-18] != b"PK\x05\x06":
        reject("zip")
    signature, disk, directory_disk, disk_entries, total_entries, directory_size, directory_offset, comment_size = struct.unpack(
        "<IHHHHIIH", raw[-22:]
    )
    expected_names = sorted({*bundle_binary_names(target), *BUNDLE_STATIC_FILES})
    if (
        signature != 0x06054B50
        or disk != 0
        or directory_disk != 0
        or disk_entries != 7
        or total_entries != 7
        or comment_size != 0
        or directory_offset + directory_size != len(raw) - 22
    ):
        reject("zip")
    expected_date, expected_clock, expected_stamp = zip_dos_time(epoch)
    contents: dict[str, bytes] = {}
    cursor = 0
    try:
        with zipfile.ZipFile(io.BytesIO(raw), "r") as bundle:
            if bundle.comment or [item.filename for item in bundle.infolist()] != expected_names:
                reject("zip")
            for item, expected_name in zip(bundle.infolist(), expected_names, strict=True):
                expected_mode, maximum = bundle_member_limit(expected_name, target)
                if (
                    item.filename != item.orig_filename
                    or item.header_offset != cursor
                    or item.compress_type != zipfile.ZIP_DEFLATED
                    or item.flag_bits != 0x08
                    or item.create_system != 3
                    or item.external_attr >> 16 != stat.S_IFREG | expected_mode
                    or item.extra
                    or item.comment
                    or item.date_time != expected_stamp
                    or item.file_size < 1
                    or item.file_size > maximum
                    or item.compress_size < 1
                ):
                    reject("zip")
                if cursor + 30 > len(raw):
                    reject("zip")
                (
                    local_signature,
                    needed_version,
                    flags,
                    method,
                    modified_time,
                    modified_date,
                    local_crc,
                    local_compressed,
                    local_size,
                    name_size,
                    extra_size,
                ) = struct.unpack("<IHHHHHIIIHH", raw[cursor : cursor + 30])
                name_start = cursor + 30
                name_end = name_start + name_size
                data_start = name_end + extra_size
                try:
                    local_name = raw[name_start:name_end].decode("ascii", "strict")
                except UnicodeError as exc:
                    raise VerificationError("zip") from exc
                if (
                    local_signature != 0x04034B50
                    or needed_version != 20
                    or flags != 0x08
                    or method != zipfile.ZIP_DEFLATED
                    or modified_time != expected_clock
                    or modified_date != expected_date
                    or local_crc != 0
                    or local_compressed != 0
                    or local_size != 0
                    or local_name != expected_name
                    or extra_size != 0
                ):
                    reject("zip")
                descriptor_start = data_start + item.compress_size
                descriptor_end = descriptor_start + 16
                if descriptor_end > len(raw):
                    reject("zip")
                descriptor = struct.unpack("<IIII", raw[descriptor_start:descriptor_end])
                if descriptor != (0x08074B50, item.CRC, item.compress_size, item.file_size):
                    reject("zip")
                content = bundle.read(item)
                if len(content) != item.file_size:
                    reject("zip")
                contents[expected_name] = content
                cursor = descriptor_end
    except (OSError, RuntimeError, zipfile.BadZipFile, zlib.error) as exc:
        raise VerificationError("zip") from exc
    if cursor != directory_offset:
        reject("zip")
    return contents


def decode_uvarint(data: bytes, offset: int) -> tuple[int, int]:
    value = 0
    for index in range(10):
        position = offset + index
        if position >= len(data):
            reject("buildinfo")
        byte = data[position]
        if index == 9 and byte > 1:
            reject("buildinfo")
        value |= (byte & 0x7F) << (7 * index)
        if byte < 0x80:
            if index > 0 and byte == 0:
                reject("buildinfo")
            return value, position + 1
    reject("buildinfo")


def embedded_build_info(content: bytes) -> tuple[str, str]:
    magic = b"\xff Go buildinf:"
    positions: list[int] = []
    start = 0
    while True:
        position = content.find(magic, start)
        if position < 0:
            break
        if position % 16 == 0:
            positions.append(position)
        start = position + 1
    if len(positions) != 1:
        reject("buildinfo")
    position = positions[0]
    if position + 32 > len(content):
        reject("buildinfo")
    pointer_size = content[position + 14]
    flags = content[position + 15]
    if pointer_size not in (4, 8) or flags != 2:
        reject("buildinfo")
    version_size, cursor = decode_uvarint(content, position + 32)
    if version_size < 1 or version_size > 128 or cursor + version_size > len(content):
        reject("buildinfo")
    version_raw = content[cursor : cursor + version_size]
    cursor += version_size
    module_size, cursor = decode_uvarint(content, cursor)
    if module_size < 33 or module_size > 4 * 1024 * 1024 or cursor + module_size > len(content):
        reject("buildinfo")
    module_raw = content[cursor : cursor + module_size]
    start_sentinel = bytes.fromhex("3077af0c9274080241e1c107e6d618e6")
    end_sentinel = bytes.fromhex("f932433186182072008242104116d8f2")
    if not module_raw.startswith(start_sentinel) or not module_raw.endswith(end_sentinel):
        reject("buildinfo")
    try:
        version = version_raw.decode("utf-8", "strict")
        module = module_raw[16:-16].decode("utf-8", "strict")
    except UnicodeError as exc:
        raise VerificationError("buildinfo") from exc
    return version, module


def validate_module_info(module: str, expected_path: str, goos: str, goarch: str) -> None:
    if not module.endswith("\n") or "\r" in module or "\0" in module:
        reject("buildinfo")
    path_seen = False
    module_seen = False
    previous_module = ""
    settings: dict[str, str] = {}
    allowed_settings = {
        "-buildmode", "-compiler", "-trimpath", "CGO_ENABLED", "GOARCH", "GOOS",
        "GOAMD64", "GOARM64", "DefaultGODEBUG",
    }
    for line in module[:-1].split("\n"):
        if line.startswith("path\t"):
            if path_seen or line != f"path\t{expected_path}":
                reject("buildinfo")
            path_seen = True
            previous_module = ""
        elif line.startswith("mod\t"):
            fields = line[4:].split("\t")
            if module_seen or fields != ["github.com/UnbalancedCat/ipgw-meta", "(devel)", ""]:
                reject("buildinfo")
            module_seen = True
            previous_module = "main"
        elif line.startswith("dep\t"):
            fields = line[4:].split("\t")
            if len(fields) != 3 or not fields[0] or not fields[1] or any(not field.isprintable() for field in fields):
                reject("buildinfo")
            previous_module = "dependency"
        elif line.startswith("=>\t"):
            fields = line[3:].split("\t")
            if previous_module != "dependency" or len(fields) != 3 or not fields[0] or any(not field.isprintable() for field in fields):
                reject("buildinfo")
            previous_module = ""
        elif line.startswith("build\t"):
            pair = line[6:]
            if "=" not in pair or pair[:1] == '"' or pair.startswith(chr(96)):
                reject("buildinfo")
            key, value = pair.split("=", 1)
            if key not in allowed_settings or key in settings or key.startswith("vcs") or not value or any(character.isspace() for character in value):
                reject("buildinfo")
            settings[key] = value
            previous_module = ""
        else:
            reject("buildinfo")
    expected = {
        "-buildmode": "exe",
        "-compiler": "gc",
        "-trimpath": "true",
        "CGO_ENABLED": "0",
        "GOARCH": goarch,
        "GOOS": goos,
        "GOAMD64" if goarch == "amd64" else "GOARM64": "v1" if goarch == "amd64" else "v8.0",
    }
    if not path_seen or not module_seen:
        reject("buildinfo")
    for key, value in expected.items():
        if settings.get(key) != value:
            reject("buildinfo")
    unexpected_arch = "GOARM64" if goarch == "amd64" else "GOAMD64"
    if unexpected_arch in settings or set(settings) - {*expected, "DefaultGODEBUG"}:
        reject("buildinfo")

def c_string(value: bytes) -> str:
    end = value.find(b"\0")
    if end < 0:
        end = len(value)
    elif any(value[end:]):
        reject("executable")
    try:
        return value[:end].decode("ascii", "strict")
    except UnicodeError as exc:
        raise VerificationError("executable") from exc


def validate_elf(content: bytes, goarch: str) -> None:
    if len(content) < 64 or content[:6] != b"\x7fELF\x02\x01" or content[6] != 1:
        reject("elf")
    try:
        (
            executable_type,
            machine,
            version,
            _entry,
            _program_offset,
            section_offset,
            _flags,
            header_size,
            _program_entry_size,
            _program_count,
            section_entry_size,
            section_count,
            section_names_index,
        ) = struct.unpack("<HHIQQQIHHHHHH", content[16:64])
    except struct.error as exc:
        raise VerificationError("elf") from exc
    expected_machine = 62 if goarch == "amd64" else 183
    if executable_type != 2 or machine != expected_machine or version != 1 or header_size != 64:
        reject("elf")
    if section_count == 0:
        if section_offset != 0 or section_names_index != 0:
            reject("elf")
        return
    if (
        section_entry_size != 64
        or section_count > 4096
        or section_names_index >= section_count
        or section_offset < 64
        or section_offset + section_entry_size * section_count > len(content)
    ):
        reject("elf")
    sections: list[tuple[int, int, int, int]] = []
    for index in range(section_count):
        offset = section_offset + index * section_entry_size
        try:
            name_offset, section_type, _flags, _address, file_offset, size, _link, _info, _alignment, _entry_size = struct.unpack(
                "<IIQQQQIIQQ", content[offset : offset + 64]
            )
        except struct.error as exc:
            raise VerificationError("elf") from exc
        if file_offset + size > len(content) and section_type != 8:
            reject("elf")
        sections.append((name_offset, section_type, file_offset, size))
    _, names_type, names_offset, names_size = sections[section_names_index]
    if names_type != 3 or names_size < 1 or names_offset + names_size > len(content):
        reject("elf")
    names = content[names_offset : names_offset + names_size]
    for name_offset, _, _, _ in sections:
        if name_offset >= len(names):
            reject("elf")
        end = names.find(b"\0", name_offset)
        if end < 0:
            reject("elf")
        try:
            name = names[name_offset:end].decode("ascii", "strict")
        except UnicodeError as exc:
            raise VerificationError("elf") from exc
        if name == ".symtab" or name.startswith(".debug_") or name.startswith(".zdebug_"):
            reject("elf")


def validate_pe(content: bytes, goarch: str) -> None:
    if len(content) < 64 or content[:2] != b"MZ":
        reject("pe")
    pe_offset = struct.unpack("<I", content[0x3C:0x40])[0]
    if pe_offset < 64 or pe_offset + 24 > len(content) or content[pe_offset : pe_offset + 4] != b"PE\0\0":
        reject("pe")
    try:
        machine, section_count, _timestamp, symbol_offset, symbol_count, optional_size, _characteristics = struct.unpack(
            "<HHIIIHH", content[pe_offset + 4 : pe_offset + 24]
        )
    except struct.error as exc:
        raise VerificationError("pe") from exc
    expected_machine = 0x8664 if goarch == "amd64" else 0xAA64
    section_start = pe_offset + 24 + optional_size
    if (
        machine != expected_machine
        or section_count < 1
        or section_count > 96
        or symbol_offset != 0
        or symbol_count != 0
        or optional_size < 112
        or section_start + section_count * 40 > len(content)
        or struct.unpack("<H", content[pe_offset + 24 : pe_offset + 26])[0] != 0x20B
    ):
        reject("pe")
    for index in range(section_count):
        offset = section_start + index * 40
        name = c_string(content[offset : offset + 8])
        raw_size, raw_offset = struct.unpack("<II", content[offset + 16 : offset + 24])
        if name.startswith(".debug") or name.startswith("/") or (raw_size and raw_offset + raw_size > len(content)):
            reject("pe")


def validate_macho(content: bytes, goarch: str) -> None:
    if len(content) < 32:
        reject("macho")
    try:
        magic, cpu_type, _cpu_subtype, file_type, command_count, command_size, _flags, _reserved = struct.unpack(
            "<IiiIIIII", content[:32]
        )
    except struct.error as exc:
        raise VerificationError("macho") from exc
    expected_cpu = 0x01000007 if goarch == "amd64" else 0x0100000C
    if (
        magic != 0xFEEDFACF
        or cpu_type != expected_cpu
        or file_type != 2
        or command_count < 1
        or command_count > 4096
        or command_size < 8
        or 32 + command_size > len(content)
    ):
        reject("macho")
    cursor = 32
    symbol_table: tuple[int, int, int, int] | None = None
    for _ in range(command_count):
        if cursor + 8 > 32 + command_size:
            reject("macho")
        command, size = struct.unpack("<II", content[cursor : cursor + 8])
        if size < 8 or size % 8 != 0 or cursor + size > 32 + command_size:
            reject("macho")
        if command == 0x19:
            if size < 72:
                reject("macho")
            (
                _command,
                _size,
                segment_name_raw,
                _address,
                _memory_size,
                file_offset,
                file_size,
                _maximum_protection,
                _initial_protection,
                section_count,
                _segment_flags,
            ) = struct.unpack("<II16sQQQQiiII", content[cursor : cursor + 72])
            segment_name = c_string(segment_name_raw)
            if section_count > 4096 or size != 72 + section_count * 80 or file_offset + file_size > len(content):
                reject("macho")
            for index in range(section_count):
                section_offset = cursor + 72 + index * 80
                section_name_raw, section_segment_raw, _address, section_size, section_file_offset, _alignment, _relocation_offset, _relocation_count, _section_flags, _reserved1, _reserved2, _reserved3 = struct.unpack(
                    "<16s16sQQIIIIIIII", content[section_offset : section_offset + 80]
                )
                section_name = c_string(section_name_raw)
                section_segment = c_string(section_segment_raw)
                section_type = _section_flags & 0xFF
                stored_section = section_type not in {0x01, 0x0C, 0x12}
                if section_segment != segment_name or (stored_section and section_size and section_file_offset + section_size > len(content)):
                    reject("macho")
                if section_segment == "__DWARF" or section_name.startswith("__debug_"):
                    reject("macho")
        elif command == 0x02:
            if size != 24 or symbol_table is not None:
                reject("macho")
            _, _, symbol_offset, symbol_count, strings_offset, strings_size = struct.unpack(
                "<IIIIII", content[cursor : cursor + 24]
            )
            symbol_table = (symbol_offset, symbol_count, strings_offset, strings_size)
        cursor += size
    if cursor != 32 + command_size:
        reject("macho")
    if symbol_table is not None:
        symbol_offset, symbol_count, strings_offset, strings_size = symbol_table
        if symbol_count > 1_000_000 or symbol_offset + symbol_count * 16 > len(content) or strings_offset + strings_size > len(content):
            reject("macho")
        for index in range(symbol_count):
            offset = symbol_offset + index * 16
            string_index, symbol_type, section, _description, value = struct.unpack(
                "<IBBHQ", content[offset : offset + 16]
            )
            if (
                string_index >= strings_size
                or symbol_type & 0xE0
                or symbol_type & 0x0E
                or not symbol_type & 0x01
                or section != 0
                or value != 0
            ):
                reject("macho")


def validate_go_binary(content: bytes, target: str, command: str, helper: bool) -> None:
    if target not in TARGETS or len(content) < 1 or len(content) > MAX_BINARY_BYTES:
        reject("binary")
    goos, goarch = target.split("-", 1)
    if goos == "linux":
        validate_elf(content, goarch)
    elif goos == "windows":
        validate_pe(content, goarch)
    elif goos == "darwin":
        validate_macho(content, goarch)
    else:
        reject("binary")
    version, module = embedded_build_info(content)
    if version != "go1.25.0":
        reject("buildinfo")
    if helper:
        if command != "ipgw-live-gate":
            reject("buildinfo")
        path = "github.com/UnbalancedCat/ipgw-meta/internal/cmd/ipgw-live-gate"
    else:
        if command not in {"ipgw", "ipgw-meta", "ipgw-legacy"} or VERSION.encode("ascii") not in content:
            reject("buildinfo")
        path = f"github.com/UnbalancedCat/ipgw-meta/cmd/{command}"
    validate_module_info(module, path, goos, goarch)


def validate_bundle(path: Path, target: str, epoch: int) -> dict[str, dict[str, object]]:
    raw = read_regular(path, MAX_ARCHIVE_BYTES)
    contents = parse_zip_bundle(raw, target, epoch) if target.startswith("windows-") else parse_tar_bundle(raw, target, epoch)
    metrics = validate_bundle_contents(contents, target)
    for command in ("ipgw", "ipgw-meta", "ipgw-legacy"):
        name = f"{command}.exe" if target.startswith("windows-") else command
        validate_go_binary(contents[name], target, command, False)
    return metrics


def candidate_member_limit(name: str) -> int:
    if name.endswith(".tar.gz") or name.endswith(".zip"):
        return MAX_ARCHIVE_BYTES
    if name.startswith("test-tools/"):
        return MAX_BINARY_BYTES
    if name in {"release/install.ps1", "release/install.sh"}:
        return MAX_INSTALLER_BYTES
    return MAX_JSON_BYTES

def load_lock_only(path: Path) -> dict:
    return validate_lock_shape(load_canonical(path))


def validate_release(
    repository_root: Path,
    lock: dict,
    candidate_root: Path,
    release_path: Path,
    download_directory: Path,
    expect_draft: bool,
) -> None:
    release_assets = validate_candidate_root(candidate_root, lock, repository_root)
    release = load_untrusted_json(release_path, 8 * 1024 * 1024)
    if not isinstance(release, dict) or not is_positive_int(release.get("id")):
        reject("release")
    release_id = release["id"]
    expected_body = RELEASE_NOTES.decode("utf-8")
    if (
        release.get("url") != f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/{release_id}"
        or release.get("assets_url") != f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/{release_id}/assets"
        or release.get("tag_name") != VERSION
        or release.get("name") != "IPGW-Meta v1.0.0"
        or release.get("body") != expected_body
        or release.get("draft") is not expect_draft
        or release.get("prerelease") is not False
    ):
        reject("release")
    if expect_draft:
        if release.get("published_at") is not None:
            reject("release")
    else:
        validate_timestamp(release.get("published_at"))
    expected: dict[str, dict] = {
        name: release_assets[f"release/{name}"]
        for name in PUBLIC_BASENAMES
    }
    assets = release.get("assets")
    if not isinstance(assets, list) or len(assets) != len(expected):
        reject("release-assets")
    seen_names: set[str] = set()
    seen_ids: set[int] = set()
    for asset in assets:
        if not isinstance(asset, dict):
            reject("release-assets")
        name = asset.get("name")
        asset_id = asset.get("id")
        if name not in expected or name in seen_names or not is_positive_int(asset_id) or asset_id in seen_ids:
            reject("release-assets")
        metric = expected[name]
        expected_digest = f"sha256:{metric['sha256']}"
        if (
            asset.get("state") != "uploaded"
            or asset.get("size") != metric["size"]
            or asset.get("url") != f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/assets/{asset_id}"
            or (asset.get("digest") is not None and asset.get("digest") != expected_digest)
        ):
            reject("release-assets")
        seen_names.add(name)
        seen_ids.add(asset_id)
    if seen_names != set(expected) or len({name.lower() for name in seen_names}) != len(seen_names):
        reject("release-assets")
    download_directory = download_directory.resolve(strict=True)
    if exact_tree(download_directory, set()) != set(expected):
        reject("download")
    for name, metric in expected.items():
        size, digest = sha256_file(download_directory / name, candidate_member_limit(f"release/{name}"))
        if size != metric["size"] or digest != metric["sha256"]:
            reject("download")


def append_outputs(path: Path, values: dict[str, object]) -> None:
    lines: list[str] = []
    for key, raw_value in values.items():
        value = str(raw_value)
        if re.fullmatch(r"[a-z][a-z0-9_]{0,63}", key) is None:
            reject("output")
        try:
            encoded = value.encode("ascii", "strict")
        except UnicodeError as exc:
            raise VerificationError("output") from exc
        if not encoded or len(encoded) > 4096 or any(byte < 0x21 or byte > 0x7E for byte in encoded):
            reject("output")
        lines.append(f"{key}={value}\n")
    payload = "".join(lines).encode("ascii")
    try:
        before = path.lstat()
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > 4 * 1024 * 1024:
            reject("output")
        flags = os.O_WRONLY | os.O_APPEND
        if hasattr(os, "O_CLOEXEC"):
            flags |= os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        descriptor = os.open(path, flags)
        try:
            after = os.fstat(descriptor)
            if not stat.S_ISREG(after.st_mode) or after.st_nlink != 1 or (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                reject("output")
            written = 0
            while written < len(payload):
                count = os.write(descriptor, payload[written:])
                if count < 1:
                    reject("output")
                written += count
        finally:
            os.close(descriptor)
    except OSError as exc:
        raise VerificationError("output") from exc


def lock_outputs(lock: dict) -> dict[str, object]:
    workflow = lock["workflow"]
    artifact = lock["artifact"]
    return {
        "version": lock["version"],
        "candidate_id": lock["candidate_id"],
        "source_commit": lock["source_commit"],
        "source_tree": lock["source_tree"],
        "candidate_run_id": workflow["run_id"],
        "candidate_run_attempt": workflow["run_attempt"],
        "artifact_id": artifact["id"],
        "artifact_name": artifact["name"],
        "artifact_digest": artifact["digest"],
        "candidate_set_sha256": lock["candidate_set_sha256"],
        "release_manifest_sha256": lock["release_manifest_sha256"],
        "build_input_sha256": lock["build_input_sha256"],
    }

class ClosedArgumentParser(argparse.ArgumentParser):
    def error(self, message: str) -> None:
        del message
        reject("arguments")


def build_parser() -> argparse.ArgumentParser:
    parser = ClosedArgumentParser(prog="promotion.py")
    commands = parser.add_subparsers(dest="command", required=True)

    lock = commands.add_parser("lock")
    lock.add_argument("--repository-root", required=True, type=Path)
    lock.add_argument("--lock", required=True, type=Path)
    lock.add_argument("--notes", required=True, type=Path)
    lock.add_argument("--tag-commit", required=True)
    lock.add_argument("--github-output", required=True, type=Path)

    api = commands.add_parser("api")
    api.add_argument("--lock", required=True, type=Path)
    api.add_argument("--run-json", required=True, type=Path)
    api.add_argument("--artifact-json", required=True, type=Path)

    artifact = commands.add_parser("artifact")
    artifact.add_argument("--repository-root", required=True, type=Path)
    artifact.add_argument("--lock", required=True, type=Path)
    artifact.add_argument("--archive", required=True, type=Path)
    artifact.add_argument("--destination", required=True, type=Path)

    candidate = commands.add_parser("candidate")
    candidate.add_argument("--repository-root", required=True, type=Path)
    candidate.add_argument("--lock", required=True, type=Path)
    candidate.add_argument("--candidate-root", required=True, type=Path)

    attestations = commands.add_parser("attestations")
    attestations.add_argument("--lock", required=True, type=Path)
    attestations.add_argument("--candidate-json", required=True, type=Path)
    attestations.add_argument("--public-directory", required=True, type=Path)

    release = commands.add_parser("release")
    release.add_argument("--repository-root", required=True, type=Path)
    release.add_argument("--lock", required=True, type=Path)
    release.add_argument("--candidate-root", required=True, type=Path)
    release.add_argument("--release-json", required=True, type=Path)
    release.add_argument("--download-directory", required=True, type=Path)
    release.add_argument("--expect-draft", required=True, choices=("true", "false"))
    return parser


def run(arguments: list[str]) -> int:
    options = build_parser().parse_args(arguments)
    if options.command == "lock":
        lock = validate_lock(
            options.repository_root,
            options.lock,
            options.notes,
            options.tag_commit,
        )
        append_outputs(options.github_output, lock_outputs(lock))
    elif options.command == "api":
        lock = load_lock_only(options.lock)
        validate_candidate_api(lock, options.run_json, options.artifact_json)
    elif options.command == "artifact":
        lock = load_lock_only(options.lock)
        extract_artifact(
            options.archive,
            options.destination,
            lock,
            options.repository_root,
        )
    elif options.command == "candidate":
        lock = load_lock_only(options.lock)
        validate_candidate_root(options.candidate_root, lock, options.repository_root)
    elif options.command == "attestations":
        lock = load_lock_only(options.lock)
        validate_attestations(lock, options.candidate_json, options.public_directory)
    elif options.command == "release":
        lock = load_lock_only(options.lock)
        validate_release(
            options.repository_root,
            lock,
            options.candidate_root,
            options.release_json,
            options.download_directory,
            options.expect_draft == "true",
        )
    else:
        reject("arguments")
    return 0


def main() -> int:
    try:
        return run(sys.argv[1:])
    except (Exception, KeyboardInterrupt):
        print("promotion: verification failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

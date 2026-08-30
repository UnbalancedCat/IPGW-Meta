#!/usr/bin/env python3
"""Synthetic, offline tests for the closed-world promotion verifier."""

from __future__ import annotations

import datetime as dt
import hashlib
import importlib.util
import io
import json
import os
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import unittest
import zipfile
import zlib
from pathlib import Path

sys.dont_write_bytecode = True

SCRIPT = Path(__file__).with_name("promotion.py")
SPEC = importlib.util.spec_from_file_location("ipgw_promotion", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("promotion test import failed")
P = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(P)


def write_file(path: Path, data: bytes, mode: int = 0o644) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    os.chmod(path, mode)


def run_git(root: Path, *arguments: str, environment: dict[str, str] | None = None) -> str:
    env = os.environ.copy()
    if environment:
        env.update(environment)
    completed = subprocess.run(
        ["git", *arguments],
        cwd=root,
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError("synthetic git command failed")
    return completed.stdout.decode("ascii", "strict").strip()


def metric(data: bytes) -> dict[str, object]:
    return {"size": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def uvarint(value: int) -> bytes:
    result = bytearray()
    while value >= 0x80:
        result.append((value & 0x7F) | 0x80)
        value >>= 7
    result.append(value)
    return bytes(result)


def build_info_blob(path: str, goos: str, goarch: str) -> bytes:
    knob = f"GOAMD64=v1" if goarch == "amd64" else "GOARM64=v8.0"
    module = (
        f"path\t{path}\n"
        "mod\tgithub.com/UnbalancedCat/ipgw-meta\t(devel)\t\n"
        "build\t-buildmode=exe\n"
        "build\t-compiler=gc\n"
        "build\t-trimpath=true\n"
        "build\tCGO_ENABLED=0\n"
        f"build\tGOARCH={goarch}\n"
        f"build\tGOOS={goos}\n"
        f"build\t{knob}\n"
    ).encode("utf-8")
    framed = (
        bytes.fromhex("3077af0c9274080241e1c107e6d618e6")
        + module
        + bytes.fromhex("f932433186182072008242104116d8f2")
    )
    version = b"go1.25.0"
    header = b"\xff Go buildinf:" + bytes((8, 2)) + bytes(16)
    return header + uvarint(len(version)) + version + uvarint(len(framed)) + framed


def append_aligned(prefix: bytes, blob: bytes, include_version: bool) -> bytes:
    padding = (-len(prefix)) % 16
    suffix = b"\0v1.0.0\0" if include_version else b"\0"
    return prefix + bytes(padding) + blob + suffix


def synthetic_binary(target: str, command: str, helper: bool = False) -> bytes:
    goos, goarch = target.split("-", 1)
    path = (
        "github.com/UnbalancedCat/ipgw-meta/internal/cmd/ipgw-live-gate"
        if helper
        else f"github.com/UnbalancedCat/ipgw-meta/cmd/{command}"
    )
    blob = build_info_blob(path, goos, goarch)
    if goos == "linux":
        machine = 62 if goarch == "amd64" else 183
        header = b"\x7fELF\x02\x01\x01" + bytes(9)
        header += struct.pack("<HHIQQQIHHHHHH", 2, machine, 1, 0, 0, 0, 0, 64, 0, 0, 0, 0, 0)
        return append_aligned(header, blob, not helper)
    if goos == "windows":
        machine = 0x8664 if goarch == "amd64" else 0xAA64
        image = bytearray(512)
        image[:2] = b"MZ"
        struct.pack_into("<I", image, 0x3C, 0x80)
        image[0x80:0x84] = b"PE\0\0"
        struct.pack_into("<HHIIIHH", image, 0x84, machine, 1, 0, 0, 0, 112, 0)
        struct.pack_into("<H", image, 0x98, 0x20B)
        section = 0x80 + 24 + 112
        image[section : section + 8] = b".data\0\0\0"
        return append_aligned(bytes(image), blob, not helper)
    if goos == "darwin":
        cpu = 0x01000007 if goarch == "amd64" else 0x0100000C
        header = struct.pack("<IiiIIIII", 0xFEEDFACF, cpu, 0, 2, 1, 72, 0, 0)
        segment = struct.pack("<II16sQQQQiiII", 0x19, 72, b"__DATA" + bytes(10), 0, 0, 0, 0, 3, 3, 0, 0)
        return append_aligned(header + segment, blob, not helper)
    raise AssertionError(target)


def tar_octal(value: int, size: int) -> bytes:
    return f"{value:0{size - 1}o}".encode("ascii") + b"\0"


def tar_header(name: str, mode: int, data: bytes, epoch: int) -> bytes:
    header = bytearray(512)
    name_raw = name.encode("ascii")
    header[: len(name_raw)] = name_raw
    header[100:108] = tar_octal(mode, 8)
    header[108:116] = tar_octal(0, 8)
    header[116:124] = tar_octal(0, 8)
    header[124:136] = tar_octal(len(data), 12)
    header[136:148] = tar_octal(epoch, 12)
    header[148:156] = b"        "
    header[156:157] = b"0"
    header[257:263] = b"ustar\0"
    header[263:265] = b"00"
    header[329:337] = tar_octal(0, 8)
    header[337:345] = tar_octal(0, 8)
    checksum = sum(header)
    header[148:156] = f"{checksum:06o}".encode("ascii") + b"\0 "
    return bytes(header)


def tar_gzip(contents: dict[str, bytes], target: str, epoch: int) -> bytes:
    payload = bytearray()
    for name in sorted(contents):
        mode, _ = P.bundle_member_limit(name, target)
        data = contents[name]
        payload.extend(tar_header(name, mode, data, epoch))
        payload.extend(data)
        payload.extend(bytes((-len(data)) % 512))
    payload.extend(bytes(1024))
    compressor = zlib.compressobj(9, zlib.DEFLATED, -zlib.MAX_WBITS)
    compressed = compressor.compress(bytes(payload)) + compressor.flush()
    header = struct.pack("<BBBBIBB", 0x1F, 0x8B, 8, 0, epoch, 2, 255)
    trailer = struct.pack("<II", zlib.crc32(payload), len(payload) & 0xFFFFFFFF)
    return header + compressed + trailer


class Unseekable(io.BytesIO):
    def seekable(self) -> bool:
        return False

    def seek(self, *arguments: object) -> int:
        del arguments
        raise io.UnsupportedOperation("synthetic stream is not seekable")


def zip_bundle(contents: dict[str, bytes], target: str, epoch: int) -> bytes:
    stream = Unseekable()
    _, _, timestamp = P.zip_dos_time(epoch)
    with zipfile.ZipFile(stream, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name in sorted(contents):
            mode, _ = P.bundle_member_limit(name, target)
            info = zipfile.ZipInfo(name, timestamp)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = (stat.S_IFREG | mode) << 16
            archive.writestr(info, contents[name], compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    return stream.getvalue()


def make_bundle(target: str, epoch: int) -> tuple[bytes, dict[str, dict[str, object]]]:
    contents = {
        name: synthetic_binary(target, name.removesuffix(".exe"))
        for name in P.bundle_binary_names(target)
    }
    contents["LICENSE"] = b"synthetic promotion fixture license\n"
    contents["launcher-default.yaml"] = b"schema_version: 1\nmode: meta\ncohort: new-install\n"
    contents["bundle-manifest.json"] = P.bundle_manifest_bytes(target, contents)
    values = {name: metric(data) for name, data in contents.items()}
    contents["SHA256SUMS"] = P.checksum_bytes(values)
    raw = zip_bundle(contents, target, epoch) if target.startswith("windows-") else tar_gzip(contents, target, epoch)
    return raw, values


class Fixture:
    def __init__(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        run_git(self.repository, "init", "-q")
        run_git(self.repository, "config", "user.name", "Promotion Test")
        run_git(self.repository, "config", "user.email", "promotion@example.invalid")
        run_git(self.repository, "config", "commit.gpgsign", "false")
        run_git(self.repository, "config", "core.autocrlf", "false")
        write_file(self.repository / "source.txt", b"immutable source input\n")
        run_git(self.repository, "add", "--", "source.txt")
        dates = {
            "GIT_AUTHOR_DATE": "2026-08-30T00:00:00Z",
            "GIT_COMMITTER_DATE": "2026-08-30T00:00:00Z",
        }
        run_git(self.repository, "commit", "-q", "-m", "synthetic source", environment=dates)
        self.source_commit = run_git(self.repository, "rev-parse", "HEAD")
        self.source_tree = run_git(self.repository, "rev-parse", "HEAD^{tree}")
        self.epoch = int(run_git(self.repository, "show", "-s", "--format=%ct", self.source_commit))
        self.run_id = 101
        self.run_attempt = 2
        self.candidate_id = f"{P.VERSION}-{self.source_commit[:12]}-{self.run_id}.{self.run_attempt}"
        self.build_input = P.hash_build_input(self.repository, self.source_commit)

        self.candidate = self.root / "candidate"
        (self.candidate / "release").mkdir(parents=True)
        (self.candidate / "test-tools").mkdir()
        payload_metrics: dict[str, dict[str, object]] = {}
        bundle_metrics: dict[str, dict[str, dict[str, object]]] = {}

        installers = {
            "install.ps1": b"param()\nWrite-Output synthetic\n",
            "install.sh": b"#!/usr/bin/env bash\nset -eu\nprintf synthetic\n",
        }
        for name, data in installers.items():
            write_file(self.candidate / "release" / name, data, 0o755 if name.endswith(".sh") else 0o644)
            payload_metrics[name] = metric(data)
        for target in P.TARGETS:
            extension = ".zip" if target.startswith("windows-") else ".tar.gz"
            name = f"ipgw-meta-{target}{extension}"
            data, members = make_bundle(target, self.epoch)
            write_file(self.candidate / "release" / name, data)
            payload_metrics[name] = metric(data)
            bundle_metrics[target] = members

        release_checksums = P.checksum_bytes(payload_metrics)
        write_file(self.candidate / "release" / "SHA256SUMS", release_checksums)
        release_manifest = {
            "schema_version": 1,
            "plan_id": P.PLAN_ID,
            "revision": P.REVISION,
            "version": P.VERSION,
            "candidate_id": self.candidate_id,
            "source_commit": self.source_commit,
            "source_tree": self.source_tree,
            "build_input_sha256": self.build_input,
            "release_sha256sums_sha256": hashlib.sha256(release_checksums).hexdigest(),
            "assets": [
                {
                    "name": name,
                    "platform": P.RELEASE_PAYLOADS[name],
                    "size": payload_metrics[name]["size"],
                    "sha256": payload_metrics[name]["sha256"],
                }
                for name in sorted(P.RELEASE_PAYLOADS)
            ],
        }
        release_manifest_raw = P.canonical_bytes(release_manifest)
        write_file(self.candidate / "release" / "release-manifest.json", release_manifest_raw)

        release_metrics = {
            f"release/{name}": metric((self.candidate / "release" / name).read_bytes())
            for name in P.PUBLIC_BASENAMES
        }
        tool_metrics: dict[str, dict[str, object]] = {}
        for target in ("linux-amd64", "windows-amd64"):
            name = (
                "test-tools/ipgw-live-gate-linux-amd64"
                if target == "linux-amd64"
                else "test-tools/ipgw-live-gate-windows-amd64.exe"
            )
            data = synthetic_binary(target, "ipgw-live-gate", helper=True)
            write_file(self.candidate / name, data, 0o755)
            tool_metrics[name] = metric(data)

        live_targets = []
        for target, name in (("linux-amd64", "ipgw-meta"), ("windows-amd64", "ipgw-meta.exe")):
            value = bundle_metrics[target][name]
            live_targets.append(
                {
                    "platform": target,
                    "name": name,
                    "size": value["size"],
                    "sha256": value["sha256"],
                }
            )
        candidate_manifest = {
            "schema_version": 1,
            "plan_id": P.PLAN_ID,
            "revision": P.REVISION,
            "version": P.VERSION,
            "candidate_id": self.candidate_id,
            "source_commit": self.source_commit,
            "source_tree": self.source_tree,
            "workflow_run_id": self.run_id,
            "workflow_run_attempt": self.run_attempt,
            "toolchain": {
                "go_version": "go1.25.0",
                "go_toolchain": "local",
                "host_platform": "linux-amd64",
                "cgo_enabled": False,
                "goamd64": "v1",
                "goarm64": "v8.0",
                "source_date_epoch": self.epoch,
                "build_recipe": "candidate-v1",
            },
            "build_input_sha256": self.build_input,
            "release_assets": [
                {
                    "name": name,
                    "platform": P.PUBLIC_ASSETS[name],
                    "size": release_metrics[name]["size"],
                    "sha256": release_metrics[name]["sha256"],
                }
                for name in sorted(P.PUBLIC_ASSETS)
            ],
            "test_tools": [
                {
                    "name": name,
                    "platform": P.TEST_TOOLS[name],
                    "size": tool_metrics[name]["size"],
                    "sha256": tool_metrics[name]["sha256"],
                }
                for name in sorted(P.TEST_TOOLS)
            ],
            "live_gate_targets": live_targets,
        }
        candidate_manifest_raw = P.canonical_bytes(candidate_manifest)
        write_file(self.candidate / "candidate-manifest.json", candidate_manifest_raw)
        root_metrics = {
            name: metric((self.candidate / name).read_bytes())
            for name in P.ROOT_FILES
            if name != "SHA256SUMS"
        }
        write_file(self.candidate / "SHA256SUMS", P.checksum_bytes(root_metrics))

        self.artifact_name = f"candidate-set-{self.candidate_id}"
        self.artifact = self.root / self.artifact_name
        with zipfile.ZipFile(self.artifact, "w", compression=zipfile.ZIP_STORED) as archive:
            for name in sorted(P.ROOT_FILES):
                info = zipfile.ZipInfo(name)
                info.create_system = 3
                info.external_attr = (stat.S_IFREG | 0o644) << 16
                archive.writestr(info, (self.candidate / name).read_bytes())
        self.artifact_digest = hashlib.sha256(self.artifact.read_bytes()).hexdigest()
        self.artifact_id = 77
        self.candidate_set_sha256 = hashlib.sha256(candidate_manifest_raw).hexdigest()
        self.release_manifest_sha256 = hashlib.sha256(release_manifest_raw).hexdigest()

        subjects = [
            {"name": self.artifact_name, "sha256": self.artifact_digest},
            *[
                {
                    "name": name,
                    "sha256": release_metrics[f"release/{name}"]["sha256"],
                }
                for name in P.PUBLIC_BASENAMES
            ],
        ]
        subjects.sort(key=lambda item: item["name"])
        evidence_ids = [f"EVID-20260830-{index:03d}" for index in range(1, 5)]
        self.evidence_directory = self.repository / "docs" / "evidence" / "releases" / P.VERSION
        self.evidence_directory.mkdir(parents=True)
        for evidence_id, values in zip(evidence_ids, sorted(P.REQUIRED_EVIDENCE_TUPLES), strict=True):
            platform, testbed, network_type, auth_method, suite = values
            before, after = (
                (P.PASSWORD_BEFORE, P.PASSWORD_AFTER)
                if suite == "password_core"
                else (P.QR_BEFORE, P.QR_AFTER)
            )
            fields = {
                "schema_version": 1,
                "plan_id": P.PLAN_ID,
                "revision": P.REVISION,
                "evidence_id": evidence_id,
                "candidate_id": self.candidate_id,
                "candidate_set_sha256": self.candidate_set_sha256,
                "source_commit": self.source_commit,
                "platform": platform,
                "testbed": testbed,
                "network_type": network_type,
                "auth_method": auth_method,
                "suite": suite,
                "result": "pass",
                "capability_before": before,
                "capability_after": after,
                "started_at": "2026-08-30T00:00:00Z",
                "finished_at": "2026-08-30T00:01:00Z",
                "bundle_sha256": hashlib.sha256(evidence_id.encode("ascii")).hexdigest(),
            }
            summary = {key: fields[key] for key in P.EVIDENCE_KEYS}
            write_file(self.evidence_directory / f"{evidence_id}.json", P.canonical_bytes(summary))
        self.notes = self.evidence_directory / "release-notes.md"
        write_file(self.notes, P.RELEASE_NOTES)
        self.lock_value = {
            "schema_version": 1,
            "plan_id": P.PLAN_ID,
            "revision": P.REVISION,
            "version": P.VERSION,
            "candidate_id": self.candidate_id,
            "source_commit": self.source_commit,
            "source_tree": self.source_tree,
            "workflow": {
                "repository_id": P.REPOSITORY_ID,
                "path": P.CANDIDATE_WORKFLOW,
                "run_id": self.run_id,
                "run_attempt": self.run_attempt,
            },
            "artifact": {
                "id": self.artifact_id,
                "name": self.artifact_name,
                "digest": f"sha256:{self.artifact_digest}",
            },
            "candidate_set_sha256": self.candidate_set_sha256,
            "release_manifest_sha256": self.release_manifest_sha256,
            "build_input_sha256": self.build_input,
            "attestation_subjects": subjects,
            "evidence_ids": evidence_ids,
            "release_notes_sha256": hashlib.sha256(P.RELEASE_NOTES).hexdigest(),
        }
        self.lock = self.evidence_directory / "promotion-lock.json"
        write_file(self.lock, P.canonical_bytes(self.lock_value))
        run_git(self.repository, "add", "--", "docs/evidence/releases/v1.0.0")
        promotion_dates = {
            "GIT_AUTHOR_DATE": "2026-08-30T00:02:00Z",
            "GIT_COMMITTER_DATE": "2026-08-30T00:02:00Z",
        }
        run_git(self.repository, "commit", "-q", "-m", "synthetic promotion", environment=promotion_dates)
        self.tag_commit = run_git(self.repository, "rev-parse", "HEAD")

    def close(self) -> None:
        self.temporary.cleanup()

    def write_json(self, name: str, value: object) -> Path:
        path = self.root / name
        write_file(path, json.dumps(value, separators=(",", ":")).encode("utf-8"))
        return path

    def api_documents(self) -> tuple[Path, Path]:
        run = {
            "id": self.run_id,
            "run_attempt": self.run_attempt,
            "path": P.CANDIDATE_WORKFLOW,
            "event": "workflow_dispatch",
            "head_branch": "main",
            "head_sha": self.source_commit,
            "status": "completed",
            "conclusion": "success",
            "repository": {"id": P.REPOSITORY_ID},
            "head_repository": {"id": P.REPOSITORY_ID},
        }
        artifact = {
            "id": self.artifact_id,
            "name": self.artifact_name,
            "digest": f"sha256:{self.artifact_digest}",
            "expired": False,
            "size_in_bytes": self.artifact.stat().st_size,
            "workflow_run": {
                "id": self.run_id,
                "repository_id": P.REPOSITORY_ID,
                "head_repository_id": P.REPOSITORY_ID,
                "head_branch": "main",
                "head_sha": self.source_commit,
            },
        }
        return self.write_json("run.json", run), self.write_json("artifact.json", artifact)

    def attestation_documents(self) -> tuple[Path, Path]:
        invocation = f"https://github.com/UnbalancedCat/ipgw-meta/actions/runs/{self.run_id}/attempts/{self.run_attempt}"

        def document(subjects: list[dict[str, str]]) -> list[dict[str, object]]:
            return [{
                "verificationResult": {
                    "signature": {"certificate": {"runInvocationURI": invocation}},
                    "statement": {
                        "predicateType": "https://slsa.dev/provenance/v1",
                        "subject": [
                            {"name": item["name"], "digest": {"sha256": item["sha256"]}}
                            for item in subjects
                        ],
                    },
                }
            }]

        candidate = self.write_json(
            "candidate-attestation.json",
            document([{"name": self.artifact_name, "sha256": self.artifact_digest}]),
        )
        public = self.root / "public-attestations"
        public.mkdir()
        public_subjects = [
            item for item in self.lock_value["attestation_subjects"]
            if item["name"] != self.artifact_name
        ]
        for name in P.PUBLIC_BASENAMES:
            write_file(
                public / f"{name}.json",
                json.dumps(document(public_subjects), separators=(",", ":")).encode("utf-8"),
            )
        return candidate, public

    def release_documents(self, draft: bool) -> tuple[Path, Path]:
        download = self.root / ("draft-download" if draft else "public-download")
        download.mkdir()
        assets = []
        for index, name in enumerate(sorted(P.PUBLIC_BASENAMES), start=1):
            source = self.candidate / "release" / name
            shutil.copyfile(source, download / name)
            value = metric(source.read_bytes())
            assets.append({
                "id": 1000 + index,
                "name": name,
                "state": "uploaded",
                "size": value["size"],
                "digest": f"sha256:{value['sha256']}",
                "url": f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/assets/{1000 + index}",
                "browser_download_url": f"https://github.com/UnbalancedCat/ipgw-meta/releases/download/{P.VERSION}/{name}",
            })
        release_id = 900
        release = {
            "id": release_id,
            "url": f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/{release_id}",
            "assets_url": f"https://api.github.com/repos/UnbalancedCat/ipgw-meta/releases/{release_id}/assets",
            "html_url": f"https://github.com/UnbalancedCat/ipgw-meta/releases/tag/{P.VERSION}",
            "tag_name": P.VERSION,
            "name": "IPGW-Meta v1.0.0",
            "body": P.RELEASE_NOTES.decode("utf-8"),
            "draft": draft,
            "prerelease": False,
            "published_at": None if draft else "2026-08-30T00:10:00Z",
            "assets": assets,
        }
        return self.write_json("draft-release.json" if draft else "public-release.json", release), download


class PromotionVerifierTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture = Fixture()

    def tearDown(self) -> None:
        self.fixture.close()

    def assert_rejected(self, action: object) -> None:
        with self.assertRaises(P.VerificationError):
            action()

    def test_complete_happy_path(self) -> None:
        fixture = self.fixture
        lock = P.validate_lock(
            fixture.repository,
            fixture.lock,
            fixture.notes,
            fixture.tag_commit,
        )
        run_path, artifact_path = fixture.api_documents()
        P.validate_candidate_api(lock, run_path, artifact_path)

        extracted = fixture.root / "extracted"
        P.extract_artifact(fixture.artifact, extracted, lock, fixture.repository)
        candidate_attestation, public_attestations = fixture.attestation_documents()
        P.validate_attestations(lock, candidate_attestation, public_attestations)

        for draft in (True, False):
            release_path, download = fixture.release_documents(draft)
            P.validate_release(
                fixture.repository,
                lock,
                extracted,
                release_path,
                download,
                draft,
            )

        output = fixture.root / "github-output"
        write_file(output, b"")
        exit_code = P.run([
            "lock",
            "--repository-root", str(fixture.repository),
            "--lock", str(fixture.lock),
            "--notes", str(fixture.notes),
            "--tag-commit", fixture.tag_commit,
            "--github-output", str(output),
        ])
        self.assertEqual(exit_code, 0)
        values = dict(line.split("=", 1) for line in output.read_text(encoding="ascii").splitlines())
        self.assertEqual(values["source_commit"], fixture.source_commit)
        self.assertEqual(values["artifact_digest"], f"sha256:{fixture.artifact_digest}")

    def test_lock_rejects_duplicate_and_noncanonical_json(self) -> None:
        fixture = self.fixture
        raw = fixture.lock.read_bytes()
        duplicate = raw.replace(
            b'{"schema_version":1,',
            b'{"schema_version":1,"schema_version":1,',
            1,
        )
        duplicate_path = fixture.root / "duplicate-lock.json"
        write_file(duplicate_path, duplicate)
        self.assert_rejected(lambda: P.load_canonical(duplicate_path))

        noncanonical_path = fixture.root / "noncanonical-lock.json"
        value = json.loads(raw)
        write_file(noncanonical_path, json.dumps(value, indent=2).encode("utf-8"))
        self.assert_rejected(lambda: P.load_canonical(noncanonical_path))

    def test_source_and_evidence_drift_fail_closed(self) -> None:
        fixture = self.fixture
        lock = P.load_lock_only(fixture.lock)
        write_file(fixture.repository / "outside-whitelist.txt", b"drift\n")
        run_git(fixture.repository, "add", "--", "outside-whitelist.txt")
        run_git(fixture.repository, "commit", "-q", "-m", "forbidden drift")
        drift_commit = run_git(fixture.repository, "rev-parse", "HEAD")
        self.assert_rejected(lambda: P.validate_source(fixture.repository, lock, drift_commit))

        first_id, second_id = lock["evidence_ids"][:2]
        first = json.loads((fixture.evidence_directory / f"{first_id}.json").read_bytes())
        second_path = fixture.evidence_directory / f"{second_id}.json"
        second = json.loads(second_path.read_bytes())
        for key in ("platform", "testbed", "network_type", "auth_method", "suite", "capability_before", "capability_after"):
            second[key] = first[key]
        write_file(second_path, P.canonical_bytes({key: second[key] for key in P.EVIDENCE_KEYS}))
        self.assert_rejected(lambda: P.validate_evidence(fixture.evidence_directory, lock))

    def test_artifact_and_candidate_tampering_fail_closed(self) -> None:
        fixture = self.fixture
        lock = P.load_lock_only(fixture.lock)
        tampered = fixture.root / "tampered-artifact"
        data = bytearray(fixture.artifact.read_bytes())
        data[len(data) // 2] ^= 1
        write_file(tampered, bytes(data))
        self.assert_rejected(
            lambda: P.extract_artifact(
                tampered,
                fixture.root / "tampered-extract",
                lock,
                fixture.repository,
            )
        )

        malicious = fixture.root / "malicious-artifact"
        with zipfile.ZipFile(malicious, "w") as archive:
            archive.writestr("../escape", b"no")
        malicious_lock = json.loads(json.dumps(lock))
        malicious_lock["artifact"]["digest"] = f"sha256:{hashlib.sha256(malicious.read_bytes()).hexdigest()}"
        self.assert_rejected(
            lambda: P.extract_artifact(
                malicious,
                fixture.root / "malicious-extract",
                malicious_lock,
                fixture.repository,
            )
        )

        extracted = fixture.root / "valid-extract"
        P.extract_artifact(fixture.artifact, extracted, lock, fixture.repository)
        installer = extracted / "release" / "install.sh"
        installer.write_bytes(installer.read_bytes() + b"tamper\n")
        self.assert_rejected(
            lambda: P.validate_candidate_root(extracted, lock, fixture.repository)
        )

    def test_api_and_attestation_identity_drift_fail_closed(self) -> None:
        fixture = self.fixture
        lock = P.load_lock_only(fixture.lock)
        run_path, artifact_path = fixture.api_documents()
        run = json.loads(run_path.read_bytes())
        run["repository"]["id"] += 1
        write_file(run_path, json.dumps(run, separators=(",", ":")).encode("utf-8"))
        self.assert_rejected(lambda: P.validate_candidate_api(lock, run_path, artifact_path))

        candidate, public = fixture.attestation_documents()
        value = json.loads(candidate.read_bytes())
        value[0]["verificationResult"]["signature"]["certificate"]["runInvocationURI"] += "/drift"
        write_file(candidate, json.dumps(value, separators=(",", ":")).encode("utf-8"))
        self.assert_rejected(lambda: P.validate_attestations(lock, candidate, public))

    def test_release_missing_asset_and_redownload_tamper_fail_closed(self) -> None:
        fixture = self.fixture
        lock = P.load_lock_only(fixture.lock)
        release_path, download = fixture.release_documents(True)
        release = json.loads(release_path.read_bytes())
        release["assets"].pop()
        write_file(release_path, json.dumps(release, separators=(",", ":")).encode("utf-8"))
        self.assert_rejected(
            lambda: P.validate_release(
                fixture.repository,
                lock,
                fixture.candidate,
                release_path,
                download,
                True,
            )
        )

        release_path, download = fixture.release_documents(False)
        target = download / sorted(P.PUBLIC_BASENAMES)[0]
        target.write_bytes(target.read_bytes() + b"tamper")
        self.assert_rejected(
            lambda: P.validate_release(
                fixture.repository,
                lock,
                fixture.candidate,
                release_path,
                download,
                False,
            )
        )

    def test_binary_and_bundle_metadata_drift_fail_closed(self) -> None:
        linux = synthetic_binary("linux-amd64", "ipgw-meta")
        self.assert_rejected(
            lambda: P.validate_go_binary(linux, "linux-arm64", "ipgw-meta", False)
        )
        raw, _ = make_bundle("windows-amd64", self.fixture.epoch)
        corrupted = bytearray(raw)
        local_time_offset = 10
        corrupted[local_time_offset] ^= 1
        self.assert_rejected(
            lambda: P.parse_zip_bundle(bytes(corrupted), "windows-amd64", self.fixture.epoch)
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)

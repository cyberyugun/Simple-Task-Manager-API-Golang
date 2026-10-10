#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import sys
from datetime import datetime, timezone
from typing import Any

TARGETS = {
    "storage_s3",
    "storage_azure",
    "storage_gcs",
    "scanner",
    "secret_vault",
    "secret_aws",
    "secret_azure",
    "secret_gcp",
    "warehouse_bigquery",
    "warehouse_snowflake",
    "warehouse_redshift",
    "warehouse_databricks",
    "ai_remote",
    "region_automation",
}
ENVIRONMENTS = {"development", "staging", "production"}


def parse_time(raw: str) -> datetime:
    value = raw.strip()
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        raise ValueError("timestamp must include timezone")
    return parsed.astimezone(timezone.utc)


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


def timestamp() -> str:
    return now_utc().isoformat().replace("+00:00", "Z")


def safe_identity(value: str, label: str) -> str:
    identity = value.strip()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._@:/ -]{1,127}", identity):
        raise ValueError(f"{label} must be 2..128 safe display characters")
    return identity


def validate_sha256(value: str, label: str) -> str:
    digest = value.strip().lower()
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError(f"{label} must be a lowercase 64-character SHA-256 digest")
    return digest


def canonical_digest(payload: dict[str, Any], digest_field: str) -> str:
    material = dict(payload)
    material.pop(digest_field, None)
    encoded = json.dumps(
        material, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def read_object(path: pathlib.Path, label: str) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"{label} must be a JSON object")
    return payload


def parse_allowlist(raw: str) -> set[str]:
    values: set[str] = set()
    normalized = raw.replace(",", " ")
    for item in normalized.split():
        values.add(safe_identity(item, "allowed approver"))
    return values


def parse_targets(raw: str) -> list[str]:
    values: list[str] = []
    seen: set[str] = set()
    normalized = raw.replace(" ", ",")
    for part in normalized.split(","):
        target = part.strip()
        if not target:
            continue
        if target not in TARGETS:
            raise ValueError(f"unsupported target {target!r}")
        if target not in seen:
            seen.add(target)
            values.append(target)
    return values


def validate_evidence(payload: dict[str, Any]) -> list[str]:
    failures: list[str] = []
    if payload.get("schema_version") != 1:
        failures.append(f"evidence schema_version must be 1, got {payload.get('schema_version')!r}")
    if payload.get("target") not in TARGETS:
        failures.append(f"unsupported evidence target {payload.get('target')!r}")
    if payload.get("environment") not in ENVIRONMENTS:
        failures.append(f"unsupported evidence environment {payload.get('environment')!r}")
    if payload.get("status") != "passed":
        failures.append("operational evidence must be passed before approval")

    submitter = str(payload.get("submitted_by", "")).strip()
    if not submitter:
        failures.append("operational evidence must include submitted_by")
    else:
        try:
            safe_identity(submitter, "submitted_by")
        except ValueError as exc:
            failures.append(str(exc))

    evidence_digest = str(payload.get("evidence_digest", "")).strip().lower()
    if not evidence_digest:
        failures.append("operational evidence must include evidence_digest")
    else:
        try:
            validate_sha256(evidence_digest, "evidence_digest")
            expected = canonical_digest(payload, "evidence_digest")
            if evidence_digest != expected:
                failures.append("evidence_digest does not match manifest content")
        except ValueError as exc:
            failures.append(str(exc))

    try:
        parse_time(str(payload.get("completed_at", "")))
        expires_at = parse_time(str(payload.get("expires_at", "")))
        if expires_at <= now_utc():
            failures.append("operational evidence is already expired")
    except ValueError as exc:
        failures.append(str(exc))

    return failures


def validate_approval(payload: dict[str, Any]) -> list[str]:
    failures: list[str] = []
    if payload.get("schema_version") != 1:
        failures.append(f"approval schema_version must be 1, got {payload.get('schema_version')!r}")
    if payload.get("kind") != "provider_operational_approval":
        failures.append("approval kind is invalid")
    if payload.get("status") != "approved":
        failures.append("approval status must be approved")
    if payload.get("target") not in TARGETS:
        failures.append(f"unsupported approval target {payload.get('target')!r}")
    if payload.get("environment") not in ENVIRONMENTS:
        failures.append(f"unsupported approval environment {payload.get('environment')!r}")

    for field in ("submitted_by", "approved_by"):
        try:
            safe_identity(str(payload.get(field, "")), field)
        except ValueError as exc:
            failures.append(str(exc))
    if payload.get("submitted_by") == payload.get("approved_by"):
        failures.append("approved_by must differ from submitted_by")

    try:
        validate_sha256(str(payload.get("evidence_digest", "")), "evidence_digest")
    except ValueError as exc:
        failures.append(str(exc))

    supersedes = str(payload.get("supersedes_digest", "") or "").strip()
    if supersedes:
        try:
            validate_sha256(supersedes, "supersedes_digest")
        except ValueError as exc:
            failures.append(str(exc))
        if supersedes == payload.get("evidence_digest"):
            failures.append("supersedes_digest must differ from evidence_digest")

    try:
        parse_time(str(payload.get("approved_at", "")))
    except ValueError as exc:
        failures.append(str(exc))

    digest = str(payload.get("approval_digest", "")).strip().lower()
    try:
        validate_sha256(digest, "approval_digest")
        if digest != canonical_digest(payload, "approval_digest"):
            failures.append("approval_digest does not match manifest content")
    except ValueError as exc:
        failures.append(str(exc))

    return failures


def validate_revocation(payload: dict[str, Any]) -> list[str]:
    failures: list[str] = []
    if payload.get("schema_version") != 1:
        failures.append(f"revocation schema_version must be 1, got {payload.get('schema_version')!r}")
    if payload.get("kind") != "provider_operational_revocation":
        failures.append("revocation kind is invalid")
    if payload.get("status") != "revoked":
        failures.append("revocation status must be revoked")
    if payload.get("target") not in TARGETS:
        failures.append(f"unsupported revocation target {payload.get('target')!r}")
    if payload.get("environment") not in ENVIRONMENTS:
        failures.append(f"unsupported revocation environment {payload.get('environment')!r}")

    try:
        safe_identity(str(payload.get("revoked_by", "")), "revoked_by")
    except ValueError as exc:
        failures.append(str(exc))
    try:
        validate_sha256(str(payload.get("evidence_digest", "")), "evidence_digest")
    except ValueError as exc:
        failures.append(str(exc))
    reason = str(payload.get("reason", "")).strip()
    if len(reason) < 5 or len(reason) > 1000:
        failures.append("revocation reason must be 5..1000 characters")
    try:
        parse_time(str(payload.get("revoked_at", "")))
    except ValueError as exc:
        failures.append(str(exc))

    digest = str(payload.get("revocation_digest", "")).strip().lower()
    try:
        validate_sha256(digest, "revocation_digest")
        if digest != canonical_digest(payload, "revocation_digest"):
            failures.append("revocation_digest does not match manifest content")
    except ValueError as exc:
        failures.append(str(exc))
    return failures


def enforce_actor(actor: str, allowlist_raw: str, require_allowlist: bool) -> tuple[str, list[str]]:
    identity = safe_identity(actor, "actor")
    allowlist = parse_allowlist(allowlist_raw)
    failures: list[str] = []
    if require_allowlist and not allowlist:
        failures.append("approver allowlist is required")
    if allowlist and identity not in allowlist:
        failures.append(f"actor {identity!r} is not in the approver allowlist")
    return identity, failures


def approve(args: argparse.Namespace) -> None:
    evidence = read_object(pathlib.Path(args.evidence), "evidence")
    failures = validate_evidence(evidence)
    approver, actor_failures = enforce_actor(
        args.approved_by, args.allowed_approvers, args.require_allowlist
    )
    failures.extend(actor_failures)

    if args.target and evidence.get("target") != args.target:
        failures.append(f"target mismatch: evidence={evidence.get('target')!r} expected={args.target!r}")
    if args.environment and evidence.get("environment") != args.environment:
        failures.append(
            f"environment mismatch: evidence={evidence.get('environment')!r} expected={args.environment!r}"
        )
    if evidence.get("submitted_by") == approver:
        failures.append("approver must be different from the evidence submitter")

    supersedes = None
    if args.supersedes_digest:
        supersedes = validate_sha256(args.supersedes_digest, "supersedes_digest")
        if supersedes == evidence.get("evidence_digest"):
            failures.append("supersedes_digest must differ from evidence_digest")

    note = args.note.strip()
    if len(note) > 1000:
        failures.append("approval note exceeds 1000 characters")

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"provider operational approval rejected: {failure}", file=sys.stderr)
        raise SystemExit(1)

    payload = {
        "schema_version": 1,
        "kind": "provider_operational_approval",
        "status": "approved",
        "target": evidence["target"],
        "environment": evidence["environment"],
        "evidence_digest": evidence["evidence_digest"],
        "submitted_by": evidence["submitted_by"],
        "approved_by": approver,
        "approved_at": timestamp(),
        "approval_note": note,
        "supersedes_digest": supersedes,
        "workflow_run_id": args.run_id or None,
        "workflow_run_attempt": args.run_attempt or None,
    }
    payload["approval_digest"] = canonical_digest(payload, "approval_digest")
    pathlib.Path(args.output).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    print(
        "Provider operational evidence APPROVED: "
        f"target={payload['target']} environment={payload['environment']} "
        f"submitter={payload['submitted_by']} approver={payload['approved_by']}"
    )


def verify_approval(args: argparse.Namespace) -> None:
    approval = read_object(pathlib.Path(args.file), "approval")
    failures = validate_approval(approval)

    evidence: dict[str, Any] | None = None
    if args.evidence:
        evidence = read_object(pathlib.Path(args.evidence), "evidence")
        failures.extend(validate_evidence(evidence))
        if approval.get("evidence_digest") != evidence.get("evidence_digest"):
            failures.append("approval evidence_digest does not match evidence")
        if approval.get("target") != evidence.get("target"):
            failures.append("approval target does not match evidence")
        if approval.get("environment") != evidence.get("environment"):
            failures.append("approval environment does not match evidence")
        if approval.get("submitted_by") != evidence.get("submitted_by"):
            failures.append("approval submitted_by does not match evidence")

    if args.require_allowlist:
        _, actor_failures = enforce_actor(
            str(approval.get("approved_by", "")),
            args.allowed_approvers,
            True,
        )
        failures.extend(actor_failures)

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"provider operational approval invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider operational approval PASSED: "
        f"target={approval.get('target')} environment={approval.get('environment')} "
        f"approver={approval.get('approved_by')}"
    )


def revoke(args: argparse.Namespace) -> None:
    evidence = read_object(pathlib.Path(args.evidence), "evidence")
    approval = read_object(pathlib.Path(args.approval), "approval")
    failures = validate_evidence(evidence)
    failures.extend(validate_approval(approval))

    if approval.get("evidence_digest") != evidence.get("evidence_digest"):
        failures.append("approval evidence_digest does not match evidence")

    revoker, actor_failures = enforce_actor(
        args.revoked_by, args.allowed_approvers, args.require_allowlist
    )
    failures.extend(actor_failures)

    reason = args.reason.strip()
    if len(reason) < 5 or len(reason) > 1000:
        failures.append("revocation reason must be 5..1000 characters")

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"provider operational revocation rejected: {failure}", file=sys.stderr)
        raise SystemExit(1)

    payload = {
        "schema_version": 1,
        "kind": "provider_operational_revocation",
        "status": "revoked",
        "target": evidence["target"],
        "environment": evidence["environment"],
        "evidence_digest": evidence["evidence_digest"],
        "approval_digest": approval["approval_digest"],
        "revoked_by": revoker,
        "revoked_at": timestamp(),
        "reason": reason,
        "workflow_run_id": args.run_id or None,
        "workflow_run_attempt": args.run_attempt or None,
    }
    payload["revocation_digest"] = canonical_digest(payload, "revocation_digest")
    pathlib.Path(args.output).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    print(
        "Provider operational evidence REVOKED: "
        f"target={payload['target']} environment={payload['environment']} "
        f"revoked_by={payload['revoked_by']}"
    )


def verify_revocation(args: argparse.Namespace) -> None:
    revocation = read_object(pathlib.Path(args.file), "revocation")
    failures = validate_revocation(revocation)

    if args.evidence:
        evidence = read_object(pathlib.Path(args.evidence), "evidence")
        failures.extend(validate_evidence(evidence))
        if revocation.get("evidence_digest") != evidence.get("evidence_digest"):
            failures.append("revocation evidence_digest does not match evidence")

    if args.approval:
        approval = read_object(pathlib.Path(args.approval), "approval")
        failures.extend(validate_approval(approval))
        if revocation.get("approval_digest") != approval.get("approval_digest"):
            failures.append("revocation approval_digest does not match approval")

    if args.require_allowlist:
        _, actor_failures = enforce_actor(
            str(revocation.get("revoked_by", "")),
            args.allowed_approvers,
            True,
        )
        failures.extend(actor_failures)

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"provider operational revocation invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider operational revocation PASSED: "
        f"target={revocation.get('target')} environment={revocation.get('environment')} "
        f"revoked_by={revocation.get('revoked_by')}"
    )


def collect_manifests(directory: pathlib.Path) -> tuple[list[dict[str, Any]], list[dict[str, Any]], list[dict[str, Any]]]:
    evidences: list[dict[str, Any]] = []
    approvals: list[dict[str, Any]] = []
    revocations: list[dict[str, Any]] = []

    if not directory.exists():
        return evidences, approvals, revocations

    for path in directory.rglob("provider-operational-evidence.json"):
        try:
            payload = read_object(path, "evidence")
            payload["_path"] = str(path)
            evidences.append(payload)
        except (OSError, ValueError, json.JSONDecodeError):
            continue
    for path in directory.rglob("provider-operational-approval.json"):
        try:
            payload = read_object(path, "approval")
            if not validate_approval(payload):
                payload["_path"] = str(path)
                approvals.append(payload)
        except (OSError, ValueError, json.JSONDecodeError):
            continue
    for path in directory.rglob("provider-operational-revocation.json"):
        try:
            payload = read_object(path, "revocation")
            if not validate_revocation(payload):
                payload["_path"] = str(path)
                revocations.append(payload)
        except (OSError, ValueError, json.JSONDecodeError):
            continue
    return evidences, approvals, revocations


def report(args: argparse.Namespace) -> None:
    directory = pathlib.Path(args.input_dir)
    evidences, approvals, revocations = collect_manifests(directory)
    required_targets = parse_targets(args.required_targets) if args.required_targets else []
    if required_targets:
        targets = required_targets
    else:
        targets = sorted(
            {
                str(item.get("target"))
                for item in evidences
                if item.get("environment") == args.environment and item.get("target") in TARGETS
            }
        )

    now = parse_time(args.now) if args.now else now_utc()
    approval_by_digest = {
        item.get("evidence_digest"): item
        for item in approvals
        if item.get("environment") == args.environment
    }
    revoked_digests = {
        item.get("evidence_digest")
        for item in revocations
        if item.get("environment") == args.environment
    }

    rows: list[dict[str, Any]] = []
    invalid = False
    expiring_count = 0

    for target in targets:
        candidates: list[tuple[datetime, dict[str, Any]]] = []
        for evidence in evidences:
            if evidence.get("target") != target or evidence.get("environment") != args.environment:
                continue
            try:
                completed = parse_time(str(evidence.get("completed_at", "")))
            except ValueError:
                continue
            candidates.append((completed, evidence))
        candidates.sort(key=lambda item: item[0], reverse=True)

        if not candidates:
            rows.append(
                {
                    "target": target,
                    "status": "not_run",
                    "expires_at": None,
                    "days_remaining": None,
                    "approved_by": None,
                    "submitted_by": None,
                    "evidence_digest": None,
                }
            )
            if required_targets:
                invalid = True
            continue

        evidence = candidates[0][1]
        digest = str(evidence.get("evidence_digest", "")).strip().lower()
        status = "passed"
        if evidence.get("status") != "passed":
            status = "failed"
        try:
            expires_at = parse_time(str(evidence.get("expires_at", "")))
            seconds_remaining = (expires_at - now).total_seconds()
            days_remaining = seconds_remaining / 86400
            if expires_at <= now:
                status = "expired"
            elif status == "passed" and days_remaining <= args.warning_days:
                status = "expiring"
                expiring_count += 1
        except ValueError:
            expires_at = None
            days_remaining = None
            status = "failed"

        if digest and digest in revoked_digests:
            status = "revoked"

        approval = approval_by_digest.get(digest)
        if args.require_approval and approval is None and status in {"passed", "expiring"}:
            status = "unapproved"

        if status in {"not_run", "failed", "expired", "revoked", "unapproved"}:
            invalid = True

        rows.append(
            {
                "target": target,
                "status": status,
                "expires_at": expires_at.isoformat().replace("+00:00", "Z") if expires_at else None,
                "days_remaining": round(days_remaining, 2) if days_remaining is not None else None,
                "approved_by": approval.get("approved_by") if approval else None,
                "submitted_by": evidence.get("submitted_by"),
                "evidence_digest": digest or None,
            }
        )

    payload = {
        "schema_version": 1,
        "generated_at": timestamp(),
        "environment": args.environment,
        "warning_days": args.warning_days,
        "require_approval": args.require_approval,
        "required_targets": required_targets,
        "status": "attention_required" if invalid or expiring_count else "healthy",
        "rows": rows,
    }
    pathlib.Path(args.output_json).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")

    markdown = [
        "# Provider Operational Evidence Expiry",
        "",
        f"Environment: {args.environment}",
        f"Warning window: {args.warning_days} days",
        f"Require approval: {'yes' if args.require_approval else 'no'}",
        f"Status: {payload['status']}",
        "",
        "| Target | Status | Days remaining | Submitter | Approver | Evidence digest |",
        "| --- | --- | ---: | --- | --- | --- |",
    ]
    for row in rows:
        days = "-" if row["days_remaining"] is None else f"{row['days_remaining']:.1f}"
        digest = row["evidence_digest"][:12] if row["evidence_digest"] else "-"
        markdown.append(
            f"| {row['target']} | {row['status']} | {days} | "
            f"{row['submitted_by'] or '-'} | {row['approved_by'] or '-'} | {digest} |"
        )
    pathlib.Path(args.output_markdown).write_text("\n".join(markdown) + "\n", encoding="utf-8")

    for row in rows:
        if row["status"] == "expiring":
            print(
                f"::warning::Operational evidence for {row['target']} expires in "
                f"{row['days_remaining']:.1f} days"
            )
        elif row["status"] in {"not_run", "failed", "expired", "revoked", "unapproved"}:
            print(f"::warning::Operational evidence for {row['target']} is {row['status']}")

    if args.fail_on_invalid and invalid:
        raise SystemExit("provider operational evidence report contains invalid required evidence")


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

approve_parser = sub.add_parser("approve")
approve_parser.add_argument("--evidence", required=True)
approve_parser.add_argument("--output", required=True)
approve_parser.add_argument("--approved-by", required=True)
approve_parser.add_argument("--allowed-approvers", default="")
approve_parser.add_argument("--require-allowlist", action="store_true")
approve_parser.add_argument("--target")
approve_parser.add_argument("--environment")
approve_parser.add_argument("--note", default="")
approve_parser.add_argument("--supersedes-digest")
approve_parser.add_argument("--run-id")
approve_parser.add_argument("--run-attempt")
approve_parser.set_defaults(func=approve)

verify_approval_parser = sub.add_parser("verify-approval")
verify_approval_parser.add_argument("--file", required=True)
verify_approval_parser.add_argument("--evidence")
verify_approval_parser.add_argument("--allowed-approvers", default="")
verify_approval_parser.add_argument("--require-allowlist", action="store_true")
verify_approval_parser.set_defaults(func=verify_approval)

revoke_parser = sub.add_parser("revoke")
revoke_parser.add_argument("--evidence", required=True)
revoke_parser.add_argument("--approval", required=True)
revoke_parser.add_argument("--output", required=True)
revoke_parser.add_argument("--revoked-by", required=True)
revoke_parser.add_argument("--allowed-approvers", default="")
revoke_parser.add_argument("--require-allowlist", action="store_true")
revoke_parser.add_argument("--reason", required=True)
revoke_parser.add_argument("--run-id")
revoke_parser.add_argument("--run-attempt")
revoke_parser.set_defaults(func=revoke)

verify_revocation_parser = sub.add_parser("verify-revocation")
verify_revocation_parser.add_argument("--file", required=True)
verify_revocation_parser.add_argument("--evidence")
verify_revocation_parser.add_argument("--approval")
verify_revocation_parser.add_argument("--allowed-approvers", default="")
verify_revocation_parser.add_argument("--require-allowlist", action="store_true")
verify_revocation_parser.set_defaults(func=verify_revocation)

report_parser = sub.add_parser("report")
report_parser.add_argument("--input-dir", required=True)
report_parser.add_argument("--environment", choices=sorted(ENVIRONMENTS), default="production")
report_parser.add_argument("--required-targets", default="")
report_parser.add_argument("--warning-days", type=int, default=14)
report_parser.add_argument("--require-approval", action="store_true")
report_parser.add_argument("--fail-on-invalid", action="store_true")
report_parser.add_argument("--now")
report_parser.add_argument("--output-json", required=True)
report_parser.add_argument("--output-markdown", required=True)
report_parser.set_defaults(func=report)

args = parser.parse_args()
try:
    if getattr(args, "warning_days", 1) < 0 or getattr(args, "warning_days", 1) > 3650:
        raise ValueError("warning-days must be between 0 and 3650")
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"provider operational governance error: {exc}", file=sys.stderr)
    raise SystemExit(1)

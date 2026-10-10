#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import math
import pathlib
import re
import sys
from datetime import datetime, timedelta, timezone
from typing import Any

TARGET_REQUIREMENTS = {
    "storage_s3": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "data_policy_review",
    ],
    "storage_azure": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "data_policy_review",
    ],
    "storage_gcs": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "data_policy_review",
    ],
    "scanner": [
        "gateway_access_policy_review",
        "real_outage_fail_closed_exercise",
    ],
    "secret_vault": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "key_policy_review",
    ],
    "secret_aws": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "key_policy_review",
    ],
    "secret_azure": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "key_policy_review",
    ],
    "secret_gcp": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "key_policy_review",
    ],
    "warehouse_bigquery": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "downstream_readback_verification",
    ],
    "warehouse_snowflake": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "downstream_readback_verification",
    ],
    "warehouse_redshift": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "downstream_readback_verification",
    ],
    "warehouse_databricks": [
        "iam_least_privilege_review",
        "real_outage_recovery_exercise",
        "downstream_readback_verification",
    ],
    "ai_remote": [
        "gateway_access_policy_review",
        "real_outage_recovery_exercise",
        "pricing_calibration",
    ],
    "region_automation": [
        "planning_gateway_iam_review",
        "external_apply_gate_review",
        "regional_game_day",
        "rpo_rto_measurement",
    ],
}

ENVIRONMENTS = {"development", "staging", "production"}
CHECK_STATUSES = {"passed", "failed"}


def parse_time(raw: str) -> datetime:
    value = raw.strip()
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        raise ValueError("timestamp must include timezone")
    return parsed.astimezone(timezone.utc)


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def validate_environment(environment: str) -> None:
    if environment not in ENVIRONMENTS:
        raise ValueError(
            f"environment must be one of: {', '.join(sorted(ENVIRONMENTS))}"
        )


def validate_reviewer(reviewer: str) -> None:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._@:/ -]{1,127}", reviewer):
        raise ValueError("reviewer must be 2..128 safe display characters")


def validate_reference(reference: str, check: str) -> None:
    if not reference.startswith("https://"):
        raise ValueError(f"{check}.reference must be an https:// URL")
    if len(reference) > 2048:
        raise ValueError(f"{check}.reference exceeds 2048 characters")


def validate_sha256(value: str, label: str) -> None:
    if not re.fullmatch(r"[0-9a-f]{64}", value):
        raise ValueError(f"{label} must be a lowercase 64-character SHA-256 digest")


def canonical_digest(payload: dict[str, Any], digest_field: str) -> str:
    material = dict(payload)
    material.pop(digest_field, None)
    encoded = json.dumps(
        material, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def parse_json_object(raw: str, label: str) -> dict[str, Any]:
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise ValueError(f"{label} must be valid JSON: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be a JSON object")
    return value


def number(value: Any, label: str, *, positive: bool = False, non_negative: bool = False) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(float(value)):
        raise ValueError(f"{label} must be a finite number")
    result = float(value)
    if positive and result <= 0:
        raise ValueError(f"{label} must be > 0")
    if non_negative and result < 0:
        raise ValueError(f"{label} must be >= 0")
    return result


def bool_value(value: Any, label: str) -> bool:
    if not isinstance(value, bool):
        raise ValueError(f"{label} must be boolean")
    return value


def validate_measurements(target: str, checks: dict[str, Any], measurements: dict[str, Any]) -> list[str]:
    failures: list[str] = []

    def check_passed(name: str) -> bool:
        item = checks.get(name)
        return isinstance(item, dict) and item.get("status") == "passed"

    if target.startswith("warehouse_") and check_passed("downstream_readback_verification"):
        try:
            expected = int(number(measurements.get("readback_expected_rows"), "readback_expected_rows", non_negative=True))
            actual = int(number(measurements.get("readback_actual_rows"), "readback_actual_rows", non_negative=True))
            checksum_match = bool_value(measurements.get("readback_checksum_match"), "readback_checksum_match")
            if expected != actual:
                failures.append(f"warehouse readback row mismatch: expected={expected} actual={actual}")
            if not checksum_match:
                failures.append("warehouse readback checksum did not match")
        except ValueError as exc:
            failures.append(str(exc))

    if target == "ai_remote" and check_passed("pricing_calibration"):
        try:
            estimated = number(measurements.get("pricing_estimated_cost_cents"), "pricing_estimated_cost_cents", positive=True)
            observed = number(measurements.get("pricing_observed_cost_cents"), "pricing_observed_cost_cents", non_negative=True)
            tolerance = number(measurements.get("pricing_tolerance_percent"), "pricing_tolerance_percent", non_negative=True)
            if tolerance > 100:
                failures.append("pricing_tolerance_percent must be <= 100")
            difference = abs(observed - estimated) / estimated * 100
            if difference > tolerance:
                failures.append(
                    f"AI pricing calibration variance {difference:.2f}% exceeds tolerance {tolerance:.2f}%"
                )
        except ValueError as exc:
            failures.append(str(exc))

    if target == "region_automation" and check_passed("rpo_rto_measurement"):
        try:
            target_rpo = number(measurements.get("target_rpo_seconds"), "target_rpo_seconds", non_negative=True)
            achieved_rpo = number(measurements.get("achieved_rpo_seconds"), "achieved_rpo_seconds", non_negative=True)
            target_rto = number(measurements.get("target_rto_seconds"), "target_rto_seconds", positive=True)
            achieved_rto = number(measurements.get("achieved_rto_seconds"), "achieved_rto_seconds", non_negative=True)
            if achieved_rpo > target_rpo:
                failures.append(
                    f"achieved RPO {achieved_rpo:g}s exceeds target {target_rpo:g}s"
                )
            if achieved_rto > target_rto:
                failures.append(
                    f"achieved RTO {achieved_rto:g}s exceeds target {target_rto:g}s"
                )
        except ValueError as exc:
            failures.append(str(exc))

    return failures


def normalize_checks(
    target: str, checks: dict[str, Any], require_reference_digests: bool = False
) -> tuple[dict[str, Any], list[str]]:
    normalized: dict[str, Any] = {}
    failures: list[str] = []
    required = TARGET_REQUIREMENTS[target]

    for name in required:
        item = checks.get(name)
        if not isinstance(item, dict):
            failures.append(f"missing required check {name}")
            continue

        status = str(item.get("status", "")).strip().lower()
        reference = str(item.get("reference", "")).strip()
        raw_reference_sha256 = item.get("reference_sha256")
        reference_sha256 = (
            str(raw_reference_sha256).strip().lower()
            if raw_reference_sha256 not in (None, "")
            else ""
        )
        notes = str(item.get("notes", "")).strip()

        if status not in CHECK_STATUSES:
            failures.append(f"{name}.status must be passed or failed")
        try:
            validate_reference(reference, name)
        except ValueError as exc:
            failures.append(str(exc))
        if reference_sha256:
            try:
                validate_sha256(reference_sha256, f"{name}.reference_sha256")
            except ValueError as exc:
                failures.append(str(exc))
        elif require_reference_digests:
            failures.append(f"{name}.reference_sha256 is required")
        if len(notes) > 1000:
            failures.append(f"{name}.notes exceeds 1000 characters")

        normalized[name] = {
            "status": status,
            "reference": reference,
            "reference_sha256": reference_sha256 or None,
            "notes": notes,
        }

    unexpected = sorted(set(checks) - set(required))
    if unexpected:
        failures.append("unexpected checks for target: " + ", ".join(unexpected))

    return normalized, failures


def create(args: argparse.Namespace) -> None:
    if args.target not in TARGET_REQUIREMENTS:
        raise ValueError(f"unsupported target {args.target!r}")
    validate_environment(args.environment)
    validate_reviewer(args.reviewer)
    if args.submitted_by:
        validate_reviewer(args.submitted_by)
    if args.valid_days <= 0 or args.valid_days > 3650:
        raise ValueError("valid-days must be between 1 and 3650")

    checks_raw = parse_json_object(args.checks_json, "checks-json")
    measurements = parse_json_object(args.measurements_json, "measurements-json")
    checks, failures = normalize_checks(
        args.target, checks_raw, args.require_reference_digests
    )
    failures.extend(validate_measurements(args.target, checks, measurements))

    for name in TARGET_REQUIREMENTS[args.target]:
        item = checks.get(name)
        if not item or item.get("status") != "passed":
            failures.append(f"required check {name} is not passed")

    completed_at = utc_now()
    expires_at = completed_at + timedelta(days=args.valid_days)

    payload = {
        "schema_version": 1,
        "status": "passed" if not failures else "failed",
        "target": args.target,
        "environment": args.environment,
        "reviewer": args.reviewer,
        "submitted_by": args.submitted_by or None,
        "completed_at": completed_at.isoformat().replace("+00:00", "Z"),
        "expires_at": expires_at.isoformat().replace("+00:00", "Z"),
        "valid_days": args.valid_days,
        "reference_digests_required": args.require_reference_digests,
        "related_commit": args.related_commit or None,
        "workflow_run_id": args.run_id or None,
        "workflow_run_attempt": args.run_attempt or None,
        "checks": checks,
        "measurements": measurements,
        "failure_reasons": list(dict.fromkeys(failures)),
        "limitations": [
            "operator_attested_external_evidence",
            "repository_does_not_independently_verify_external_reference_content",
        ],
    }
    payload["evidence_digest"] = canonical_digest(payload, "evidence_digest")

    pathlib.Path(args.output).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    print(
        "Provider operational evidence created: "
        f"target={args.target} environment={args.environment} status={payload['status']}"
    )


def verify_payload(payload: dict[str, Any], now: datetime) -> list[str]:
    failures: list[str] = []

    if payload.get("schema_version") != 1:
        failures.append(f"schema_version: expected 1, got {payload.get('schema_version')!r}")

    target = str(payload.get("target", ""))
    if target not in TARGET_REQUIREMENTS:
        failures.append(f"unsupported target {target!r}")
        return failures

    environment = str(payload.get("environment", ""))
    try:
        validate_environment(environment)
    except ValueError as exc:
        failures.append(str(exc))

    reviewer = str(payload.get("reviewer", ""))
    try:
        validate_reviewer(reviewer)
    except ValueError as exc:
        failures.append(str(exc))

    checks = payload.get("checks")
    if not isinstance(checks, dict):
        failures.append("checks must be an object")
        checks = {}
    normalized, check_failures = normalize_checks(
        target, checks, bool(payload.get("reference_digests_required", False))
    )
    failures.extend(check_failures)

    measurements = payload.get("measurements")
    if not isinstance(measurements, dict):
        failures.append("measurements must be an object")
        measurements = {}
    failures.extend(validate_measurements(target, normalized, measurements))

    for name in TARGET_REQUIREMENTS[target]:
        item = normalized.get(name)
        if not item or item.get("status") != "passed":
            failures.append(f"required check {name} is not passed")

    try:
        completed_at = parse_time(str(payload.get("completed_at", "")))
        expires_at = parse_time(str(payload.get("expires_at", "")))
        if expires_at <= completed_at:
            failures.append("expires_at must be after completed_at")
        if expires_at <= now:
            failures.append("operational evidence is expired")
    except ValueError as exc:
        failures.append(str(exc))

    recorded_reasons = payload.get("failure_reasons")
    if not isinstance(recorded_reasons, list):
        failures.append("failure_reasons must be a list")
        recorded_reasons = []

    calculated_status = "passed" if not failures else "failed"
    if payload.get("status") not in {"passed", "failed"}:
        failures.append(f"invalid status {payload.get('status')!r}")
    elif payload.get("status") != ("passed" if not recorded_reasons else "failed"):
        failures.append("status is inconsistent with recorded failure_reasons")

    limitations = payload.get("limitations")
    if not isinstance(limitations, list) or not limitations:
        failures.append("limitations must be a non-empty list")

    evidence_digest = str(payload.get("evidence_digest", "")).strip().lower()
    if evidence_digest:
        try:
            validate_sha256(evidence_digest, "evidence_digest")
            if evidence_digest != canonical_digest(payload, "evidence_digest"):
                failures.append("evidence_digest does not match manifest content")
        except ValueError as exc:
            failures.append(str(exc))

    return list(dict.fromkeys(failures))


def verify(args: argparse.Namespace) -> None:
    path = pathlib.Path(args.file)
    payload = json.loads(path.read_text(encoding="utf-8"))
    now = parse_time(args.now) if args.now else utc_now()
    failures = verify_payload(payload, now)

    if args.target is not None and payload.get("target") != args.target:
        failures.append(
            f"target: expected {args.target!r}, got {payload.get('target')!r}"
        )
    if args.environment is not None and payload.get("environment") != args.environment:
        failures.append(
            f"environment: expected {args.environment!r}, got {payload.get('environment')!r}"
        )
    if args.related_commit is not None and payload.get("related_commit") != args.related_commit:
        failures.append(
            f"related_commit: expected {args.related_commit!r}, got {payload.get('related_commit')!r}"
        )
    if args.require_integrity:
        evidence_digest = str(payload.get("evidence_digest", "")).strip().lower()
        if not evidence_digest:
            failures.append("evidence_digest is required")
    if args.require_submitter:
        submitter = str(payload.get("submitted_by", "")).strip()
        if not submitter:
            failures.append("submitted_by is required")
        else:
            try:
                validate_reviewer(submitter)
            except ValueError as exc:
                failures.append(str(exc))
    if args.require_reference_digests and payload.get("reference_digests_required") is not True:
        failures.append("reference_digests_required must be true")
    if args.require_reference_digests:
        checks_payload = payload.get("checks")
        if isinstance(checks_payload, dict):
            for name in TARGET_REQUIREMENTS.get(str(payload.get("target", "")), []):
                item = checks_payload.get(name)
                digest = str(item.get("reference_sha256", "")).strip().lower() if isinstance(item, dict) else ""
                if not digest:
                    failures.append(f"{name}.reference_sha256 is required")
                else:
                    try:
                        validate_sha256(digest, f"{name}.reference_sha256")
                    except ValueError as exc:
                        failures.append(str(exc))
    if args.require_passed and payload.get("status") != "passed":
        failures.append("provider operational evidence is not passed")

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"provider operational evidence invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider operational evidence PASSED: "
        f"target={payload.get('target')} environment={payload.get('environment')} "
        f"reviewer={payload.get('reviewer')} expires_at={payload.get('expires_at')}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

create_parser = sub.add_parser("create")
create_parser.add_argument("--output", required=True)
create_parser.add_argument("--target", required=True)
create_parser.add_argument("--environment", required=True)
create_parser.add_argument("--reviewer", required=True)
create_parser.add_argument("--submitted-by")
create_parser.add_argument("--checks-json", required=True)
create_parser.add_argument("--measurements-json", default="{}")
create_parser.add_argument("--valid-days", type=int, default=90)
create_parser.add_argument("--related-commit")
create_parser.add_argument("--run-id")
create_parser.add_argument("--run-attempt")
create_parser.add_argument("--require-reference-digests", action="store_true")
create_parser.set_defaults(func=create)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--target")
verify_parser.add_argument("--environment")
verify_parser.add_argument("--related-commit")
verify_parser.add_argument("--now")
verify_parser.add_argument("--require-passed", action="store_true")
verify_parser.add_argument("--require-integrity", action="store_true")
verify_parser.add_argument("--require-submitter", action="store_true")
verify_parser.add_argument("--require-reference-digests", action="store_true")
verify_parser.set_defaults(func=verify)

args = parser.parse_args()
try:
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"provider operational evidence error: {exc}", file=sys.stderr)
    raise SystemExit(1)

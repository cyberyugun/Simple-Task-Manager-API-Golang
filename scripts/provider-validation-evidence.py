#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import sys
from datetime import datetime, timezone

TARGET_CLAIMS = {
    "storage_s3": ["authentication", "upload", "metadata_verification", "download", "delete"],
    "storage_azure": ["authentication", "upload", "metadata_verification", "download", "delete"],
    "storage_gcs": ["authentication", "upload", "metadata_verification", "download", "delete"],
    "scanner": ["storage_fixture", "scanner_clean_result", "scanner_to_storage_reachability"],
    "secret_vault": ["create", "read", "version_rotation", "delete", "post_delete_inaccessible"],
    "secret_aws": ["create", "read", "version_rotation", "delete", "post_delete_inaccessible"],
    "secret_azure": ["create", "read", "version_rotation", "delete", "post_delete_inaccessible"],
    "secret_gcp": ["create", "read", "version_rotation", "delete", "post_delete_inaccessible"],
    "warehouse_bigquery": ["authentication", "table_reconcile", "delivery", "deterministic_idempotency"],
    "warehouse_snowflake": ["authentication", "table_reconcile", "delivery", "deterministic_idempotency"],
    "warehouse_redshift": ["authentication", "table_reconcile", "delivery", "deterministic_idempotency"],
    "warehouse_databricks": ["authentication", "table_reconcile", "delivery", "deterministic_idempotency"],
    "ai_remote": ["authentication", "classification", "structured_output", "usage_bounds", "cost_accounting", "idempotent_duplicate"],
    "region_automation": ["authentication", "signed_plan_only", "guardrails", "deterministic_request_id", "stable_plan_id"],
}

FAILURE_CHECKS = {
    "storage_s3": ["metadata_mismatch_rejected", "unsafe_configuration_rejected"],
    "storage_azure": ["unsafe_configuration_rejected"],
    "storage_gcs": ["metadata_mismatch_rejected", "unsafe_configuration_rejected"],
    "scanner": ["insecure_endpoint_rejected", "infected_or_error_fail_closed", "redirect_rejected"],
    "secret_vault": ["unsafe_configuration_rejected", "unconfigured_external_rotation_rejected"],
    "secret_aws": ["unsafe_or_incomplete_configuration_rejected"],
    "secret_azure": ["unsafe_configuration_rejected"],
    "secret_gcp": ["unsafe_configuration_rejected"],
    "warehouse_bigquery": ["transient_retry", "checkpoint_not_advanced_on_failure", "unsafe_configuration_rejected"],
    "warehouse_snowflake": ["transient_retry", "checkpoint_not_advanced_on_failure", "unsafe_configuration_rejected"],
    "warehouse_redshift": ["transient_retry", "checkpoint_not_advanced_on_failure", "unsafe_configuration_rejected"],
    "warehouse_databricks": ["transient_retry", "checkpoint_not_advanced_on_failure", "unsafe_configuration_rejected"],
    "ai_remote": ["stable_idempotency_on_retry", "malformed_result_rejected", "unsafe_configuration_rejected"],
    "region_automation": ["stable_signature_and_idempotency_on_retry", "unsafe_or_unapproved_rejected", "planning_failure_keeps_pending"],
}

TARGET_LIMITATIONS = {
    "warehouse_bigquery": ["downstream_query_readback_not_proven"],
    "warehouse_snowflake": ["downstream_query_readback_not_proven"],
    "warehouse_redshift": ["downstream_query_readback_not_proven"],
    "warehouse_databricks": ["downstream_query_readback_not_proven"],
    "ai_remote": ["provider_pricing_calibration_not_proven", "live_outage_behavior_not_proven"],
    "region_automation": ["cloud_failover_not_executed", "measured_rpo_rto_not_proven"],
}

ALLOWED_STEP_STATUS = {"success", "failure", "skipped", "cancelled"}


def sha256_file(path: str) -> str:
    file_path = pathlib.Path(path)
    if not file_path.is_file():
        raise ValueError(f"evidence log does not exist: {path}")
    digest = hashlib.sha256()
    with file_path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def validate_target(target: str) -> None:
    if target not in TARGET_CLAIMS:
        raise ValueError(f"unsupported provider validation target: {target}")


def validate_environment(environment: str) -> None:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", environment):
        raise ValueError("environment must be 1..64 characters using letters, numbers, dot, underscore or dash")


def validate_status(name: str, status: str) -> None:
    if status not in ALLOWED_STEP_STATUS:
        raise ValueError(f"{name} must be one of: {', '.join(sorted(ALLOWED_STEP_STATUS))}")


def create(args: argparse.Namespace) -> None:
    validate_target(args.target)
    validate_environment(args.environment)
    validate_status("live status", args.live_status)
    validate_status("failure-injection status", args.failure_status)

    overall_status = "passed" if args.live_status == "success" and args.failure_status == "success" else "failed"
    limitations = [
        "provider_specific_iam_policy_requires_environment_review",
        "organization_specific_compliance_not_proven",
        "real_provider_outage_recovery_not_proven",
    ]
    limitations.extend(TARGET_LIMITATIONS.get(args.target, []))

    payload = {
        "schema_version": 1,
        "status": overall_status,
        "target": args.target,
        "environment": args.environment,
        "commit": args.commit,
        "workflow_run_id": args.run_id,
        "workflow_run_attempt": args.run_attempt,
        "source_ref": args.source_ref,
        "completed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "live_contract": {
            "status": args.live_status,
            "log_sha256": sha256_file(args.live_log),
            "claims": TARGET_CLAIMS[args.target],
        },
        "failure_injection": {
            "status": args.failure_status,
            "log_sha256": sha256_file(args.failure_log),
            "checks": FAILURE_CHECKS[args.target],
            "scope": "hermetic_repository_tests",
        },
        "limitations": limitations,
    }
    pathlib.Path(args.output).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def verify(args: argparse.Namespace) -> None:
    payload = json.loads(pathlib.Path(args.file).read_text(encoding="utf-8"))
    failures: list[str] = []

    if payload.get("schema_version") != 1:
        failures.append(f"schema_version: expected 1, got {payload.get('schema_version')!r}")

    target = str(payload.get("target", ""))
    try:
        validate_target(target)
    except ValueError as exc:
        failures.append(str(exc))

    environment = str(payload.get("environment", ""))
    try:
        validate_environment(environment)
    except ValueError as exc:
        failures.append(str(exc))

    expected = {
        "target": args.target,
        "environment": args.environment,
        "commit": args.commit,
        "workflow_run_id": args.run_id,
        "workflow_run_attempt": args.run_attempt,
    }
    for key, value in expected.items():
        if value is not None and str(payload.get(key)) != str(value):
            failures.append(f"{key}: expected {value!r}, got {payload.get(key)!r}")

    for section_name in ("live_contract", "failure_injection"):
        section = payload.get(section_name)
        if not isinstance(section, dict):
            failures.append(f"{section_name}: expected object")
            continue
        status = str(section.get("status", ""))
        if status not in ALLOWED_STEP_STATUS:
            failures.append(f"{section_name}.status: invalid {status!r}")
        digest = str(section.get("log_sha256", ""))
        if not re.fullmatch(r"[0-9a-f]{64}", digest):
            failures.append(f"{section_name}.log_sha256: invalid SHA-256")

    if target in TARGET_CLAIMS:
        live_claims = set(payload.get("live_contract", {}).get("claims", []))
        missing_claims = sorted(set(TARGET_CLAIMS[target]) - live_claims)
        if missing_claims:
            failures.append("missing live claims: " + ", ".join(missing_claims))

        failure_checks = set(payload.get("failure_injection", {}).get("checks", []))
        missing_checks = sorted(set(FAILURE_CHECKS[target]) - failure_checks)
        if missing_checks:
            failures.append("missing failure-injection checks: " + ", ".join(missing_checks))

    live_status = str(payload.get("live_contract", {}).get("status", ""))
    failure_status = str(payload.get("failure_injection", {}).get("status", ""))
    expected_overall = "passed" if live_status == "success" and failure_status == "success" else "failed"
    if payload.get("status") != expected_overall:
        failures.append(f"status: expected {expected_overall!r}, got {payload.get('status')!r}")
    if args.require_passed and expected_overall != "passed":
        failures.append("provider validation evidence is not passed")

    limitations = payload.get("limitations")
    if not isinstance(limitations, list) or not limitations:
        failures.append("limitations: expected a non-empty list")

    if failures:
        for failure in failures:
            print(f"provider validation evidence invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider validation evidence PASSED: "
        f"target={target} environment={environment} status={payload.get('status')} "
        f"commit={payload.get('commit')} run={payload.get('workflow_run_id')}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

create_parser = sub.add_parser("create")
create_parser.add_argument("--output", required=True)
create_parser.add_argument("--target", required=True)
create_parser.add_argument("--environment", required=True)
create_parser.add_argument("--commit", required=True)
create_parser.add_argument("--run-id", required=True)
create_parser.add_argument("--run-attempt", required=True)
create_parser.add_argument("--source-ref", required=True)
create_parser.add_argument("--live-status", required=True)
create_parser.add_argument("--failure-status", required=True)
create_parser.add_argument("--live-log", required=True)
create_parser.add_argument("--failure-log", required=True)
create_parser.set_defaults(func=create)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--target")
verify_parser.add_argument("--environment")
verify_parser.add_argument("--commit")
verify_parser.add_argument("--run-id")
verify_parser.add_argument("--run-attempt")
verify_parser.add_argument("--require-passed", action="store_true")
verify_parser.set_defaults(func=verify)

args = parser.parse_args()
try:
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"provider validation evidence error: {exc}", file=sys.stderr)
    raise SystemExit(1)

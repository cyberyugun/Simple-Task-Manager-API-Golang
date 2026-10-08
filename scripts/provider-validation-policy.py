#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import pathlib
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

MODES = {"warn", "enforce"}


def parse_targets(raw: str) -> list[str]:
    values: list[str] = []
    seen: set[str] = set()
    normalized = raw.replace(" ", ",")
    for part in normalized.split(","):
        target = part.strip()
        if not target:
            continue
        if target not in TARGETS:
            raise ValueError(f"unsupported required target {target!r}")
        if target not in seen:
            seen.add(target)
            values.append(target)
    if not values:
        raise ValueError("at least one required target is required")
    return values


def load_registry(path: pathlib.Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("schema_version") != 1:
        raise ValueError(f"unsupported registry schema_version {payload.get('schema_version')!r}")
    cells = payload.get("cells")
    if not isinstance(cells, list):
        raise ValueError("registry cells must be a list")
    return payload


def find_cell(registry: dict[str, Any], target: str, environment: str) -> dict[str, Any] | None:
    for cell in registry["cells"]:
        if not isinstance(cell, dict):
            continue
        if cell.get("target") == target and cell.get("environment") == environment:
            return cell
    return None


def evaluate(args: argparse.Namespace) -> None:
    if args.mode not in MODES:
        raise ValueError(f"mode must be one of: {', '.join(sorted(MODES))}")

    required_targets = parse_targets(args.required_targets)
    registry = load_registry(pathlib.Path(args.registry))

    if args.expected_commit and registry.get("expected_commit") != args.expected_commit:
        raise ValueError(
            f"registry expected_commit={registry.get('expected_commit')!r} "
            f"does not match policy expected commit {args.expected_commit!r}"
        )

    decisions: list[dict[str, Any]] = []
    passed = True

    for target in required_targets:
        cell = find_cell(registry, target, args.environment)
        reasons: list[str] = []

        if cell is None:
            status = "not_run"
            reasons.append("required_target_missing_from_registry")
            commit_matches = None
            tested_commit = None
            completed_at = None
            workflow_run_id = None
            workflow_run_attempt = None
            age_hours = None
            gaps = ["provider_validation_not_run"]
        else:
            status = str(cell.get("status", ""))
            commit_matches = cell.get("commit_matches_expected")
            tested_commit = cell.get("tested_commit")
            completed_at = cell.get("completed_at")
            workflow_run_id = cell.get("workflow_run_id")
            workflow_run_attempt = cell.get("workflow_run_attempt")
            age_hours = cell.get("age_hours")
            gaps = list(cell.get("gaps") or [])

            if status != "passed":
                reasons.append(f"status_{status or 'invalid'}")
            if args.require_current_commit and commit_matches is not True:
                reasons.append("commit_drift")

        target_passed = not reasons
        if not target_passed:
            passed = False

        decisions.append(
            {
                "target": target,
                "environment": args.environment,
                "status": status,
                "policy_passed": target_passed,
                "reasons": reasons,
                "age_hours": age_hours,
                "tested_commit": tested_commit,
                "commit_matches_expected": commit_matches,
                "completed_at": completed_at,
                "workflow_run_id": workflow_run_id,
                "workflow_run_attempt": workflow_run_attempt,
                "gaps": gaps,
            }
        )

    payload = {
        "schema_version": 1,
        "generated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "status": "passed" if passed else "failed",
        "mode": args.mode,
        "environment": args.environment,
        "required_targets": required_targets,
        "expected_commit": args.expected_commit or registry.get("expected_commit"),
        "require_current_commit": args.require_current_commit,
        "registry_generated_at": registry.get("generated_at"),
        "registry_max_age_days": registry.get("max_age_days"),
        "decisions": decisions,
    }

    pathlib.Path(args.output_json).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")

    markdown = [
        "# Provider Validation Policy",
        "",
        f"Environment: {args.environment}",
        f"Mode: {args.mode}",
        f"Status: {payload['status']}",
        f"Expected commit: {payload['expected_commit'] or 'not enforced'}",
        f"Require current commit: {'yes' if args.require_current_commit else 'no'}",
        "",
        "| Target | Registry status | Policy | Age | Commit | Run | Reasons |",
        "| --- | --- | --- | ---: | --- | --- | --- |",
    ]
    for decision in decisions:
        age = "-"
        if isinstance(decision["age_hours"], (int, float)):
            age = (
                f"{decision['age_hours']:.1f}h"
                if decision["age_hours"] < 24
                else f"{decision['age_hours'] / 24:.1f}d"
            )
        commit = str(decision["tested_commit"] or "-")
        if commit != "-":
            commit = commit[:12]
        run = "-"
        if decision["workflow_run_id"]:
            run = f"{decision['workflow_run_id']}/{decision['workflow_run_attempt']}"
        reasons = ", ".join(decision["reasons"]) if decision["reasons"] else "-"
        markdown.append(
            f"| {decision['target']} | {decision['status']} | "
            f"{'passed' if decision['policy_passed'] else 'failed'} | {age} | "
            f"{commit} | {run} | {reasons} |"
        )

    pathlib.Path(args.output_markdown).write_text("\n".join(markdown) + "\n", encoding="utf-8")

    if not passed:
        message = (
            "provider validation policy failed: "
            + ", ".join(
                f"{d['target']}={'/'.join(d['reasons'])}"
                for d in decisions
                if not d["policy_passed"]
            )
        )
        if args.mode == "enforce":
            print(message, file=sys.stderr)
            raise SystemExit(1)
        print("WARNING: " + message, file=sys.stderr)
    else:
        print(
            "Provider validation policy PASSED: "
            f"environment={args.environment} targets={','.join(required_targets)} "
            f"require_current_commit={args.require_current_commit}"
        )


def verify(args: argparse.Namespace) -> None:
    payload = json.loads(pathlib.Path(args.file).read_text(encoding="utf-8"))
    failures: list[str] = []

    if payload.get("schema_version") != 1:
        failures.append(f"schema_version: expected 1, got {payload.get('schema_version')!r}")
    if payload.get("mode") not in MODES:
        failures.append(f"mode: invalid {payload.get('mode')!r}")
    if payload.get("environment") != args.environment:
        failures.append(
            f"environment: expected {args.environment!r}, got {payload.get('environment')!r}"
        )

    if args.expected_commit is not None and payload.get("expected_commit") != args.expected_commit:
        failures.append(
            f"expected_commit: expected {args.expected_commit!r}, got {payload.get('expected_commit')!r}"
        )

    required_targets = parse_targets(args.required_targets)
    if payload.get("required_targets") != required_targets:
        failures.append(
            f"required_targets: expected {required_targets!r}, got {payload.get('required_targets')!r}"
        )

    decisions = payload.get("decisions")
    if not isinstance(decisions, list):
        failures.append("decisions: expected list")
        decisions = []

    decision_targets = [
        item.get("target")
        for item in decisions
        if isinstance(item, dict)
    ]
    if decision_targets != required_targets:
        failures.append(
            f"decision targets: expected {required_targets!r}, got {decision_targets!r}"
        )

    calculated_passed = all(
        isinstance(item, dict) and item.get("policy_passed") is True for item in decisions
    ) and len(decisions) == len(required_targets)
    expected_status = "passed" if calculated_passed else "failed"
    if payload.get("status") != expected_status:
        failures.append(
            f"status: expected {expected_status!r}, got {payload.get('status')!r}"
        )

    if args.require_passed and payload.get("status") != "passed":
        failures.append("provider validation policy is not passed")

    if failures:
        for failure in failures:
            print(f"provider validation policy invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider validation policy evidence PASSED: "
        f"environment={args.environment} status={payload.get('status')} "
        f"targets={','.join(required_targets)}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

eval_parser = sub.add_parser("evaluate")
eval_parser.add_argument("--registry", required=True)
eval_parser.add_argument("--environment", required=True)
eval_parser.add_argument("--required-targets", required=True)
eval_parser.add_argument("--expected-commit")
eval_parser.add_argument("--require-current-commit", action="store_true")
eval_parser.add_argument("--mode", choices=sorted(MODES), default="enforce")
eval_parser.add_argument("--output-json", required=True)
eval_parser.add_argument("--output-markdown", required=True)
eval_parser.set_defaults(func=evaluate)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--environment", required=True)
verify_parser.add_argument("--required-targets", required=True)
verify_parser.add_argument("--expected-commit")
verify_parser.add_argument("--require-passed", action="store_true")
verify_parser.set_defaults(func=verify)

args = parser.parse_args()
try:
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"provider validation policy error: {exc}", file=sys.stderr)
    raise SystemExit(1)

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import re
import sys
from collections import Counter
from datetime import datetime, timezone
from typing import Any

TARGETS = [
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
]

DEFAULT_ENVIRONMENTS = ["development", "staging", "production"]
ALLOWED_EVIDENCE_STATUS = {"passed", "failed"}


def parse_csv(raw: str, allowed: list[str], label: str) -> list[str]:
    values: list[str] = []
    seen: set[str] = set()
    for part in raw.split(","):
        value = part.strip()
        if not value:
            continue
        if value not in allowed:
            raise ValueError(f"unsupported {label} {value!r}")
        if value not in seen:
            seen.add(value)
            values.append(value)
    if not values:
        raise ValueError(f"at least one {label} is required")
    return values


def parse_time(raw: str) -> datetime:
    value = raw.strip()
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        raise ValueError("timestamp must include timezone")
    return parsed.astimezone(timezone.utc)


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def now_utc(raw: str | None) -> datetime:
    if raw:
        return parse_time(raw)
    return datetime.now(timezone.utc)


def validate_environment(value: str) -> None:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", value):
        raise ValueError(f"invalid environment {value!r}")


def load_evidence(path: pathlib.Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("schema_version") != 1:
        raise ValueError(f"{path}: unsupported schema_version {payload.get('schema_version')!r}")

    target = str(payload.get("target", ""))
    if target not in TARGETS:
        raise ValueError(f"{path}: unsupported target {target!r}")

    environment = str(payload.get("environment", ""))
    validate_environment(environment)

    status = str(payload.get("status", ""))
    if status not in ALLOWED_EVIDENCE_STATUS:
        raise ValueError(f"{path}: unsupported status {status!r}")

    completed_at = str(payload.get("completed_at", ""))
    completed = parse_time(completed_at)

    commit = str(payload.get("commit", "")).strip()
    run_id = str(payload.get("workflow_run_id", "")).strip()
    run_attempt = str(payload.get("workflow_run_attempt", "")).strip()
    if not commit or not run_id or not run_attempt:
        raise ValueError(f"{path}: commit, workflow_run_id and workflow_run_attempt are required")

    limitations = payload.get("limitations", [])
    if not isinstance(limitations, list) or not all(isinstance(item, str) and item for item in limitations):
        raise ValueError(f"{path}: limitations must be a string list")

    return {
        "path": str(path),
        "payload": payload,
        "target": target,
        "environment": environment,
        "status": status,
        "completed_at": completed_at,
        "completed": completed,
        "commit": commit,
        "run_id": run_id,
        "run_attempt": run_attempt,
        "evidence_sha256": sha256_file(path),
        "limitations": list(limitations),
    }


def evidence_sort_key(item: dict[str, Any]) -> tuple[datetime, int]:
    try:
        attempt = int(item["run_attempt"])
    except ValueError:
        attempt = 0
    return item["completed"], attempt


def short_commit(value: str) -> str:
    return value[:12] if value else "-"


def age_label(age_hours: float | None) -> str:
    if age_hours is None:
        return "-"
    if age_hours < 24:
        return f"{age_hours:.1f}h"
    return f"{age_hours / 24:.1f}d"


def build(args: argparse.Namespace) -> None:
    input_dir = pathlib.Path(args.input_dir)
    if not input_dir.exists():
        raise ValueError(f"input directory does not exist: {input_dir}")
    if args.max_age_days <= 0:
        raise ValueError("max-age-days must be positive")

    targets = parse_csv(args.targets, TARGETS, "target")
    environments = parse_csv(args.environments, DEFAULT_ENVIRONMENTS, "environment")
    current_time = now_utc(args.now)
    max_age_seconds = args.max_age_days * 86400

    latest: dict[tuple[str, str], dict[str, Any]] = {}
    ignored: list[dict[str, str]] = []
    discovered = 0

    for path in sorted(input_dir.rglob("provider-validation-evidence.json")):
        discovered += 1
        try:
            item = load_evidence(path)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            ignored.append({"path": str(path), "reason": str(exc)})
            continue

        if item["target"] not in targets or item["environment"] not in environments:
            continue

        key = (item["target"], item["environment"])
        previous = latest.get(key)
        if previous is None or evidence_sort_key(item) > evidence_sort_key(previous):
            latest[key] = item

    cells: list[dict[str, Any]] = []
    counts: Counter[str] = Counter()

    for target in targets:
        for environment in environments:
            key = (target, environment)
            item = latest.get(key)
            if item is None:
                cell = {
                    "target": target,
                    "environment": environment,
                    "status": "not_run",
                    "age_hours": None,
                    "completed_at": None,
                    "tested_commit": None,
                    "commit_matches_expected": None,
                    "workflow_run_id": None,
                    "workflow_run_attempt": None,
                    "evidence_path": None,
                    "gaps": ["provider_validation_not_run"],
                }
            else:
                age_seconds = (current_time - item["completed"]).total_seconds()
                age_hours = max(0.0, age_seconds / 3600)
                gaps = list(dict.fromkeys(item["limitations"]))
                if age_seconds < -300:
                    gaps.append("evidence_timestamp_in_future")

                if item["status"] != "passed":
                    status = "failed"
                    gaps.append("validation_failed")
                elif age_seconds > max_age_seconds:
                    status = "expired"
                    gaps.append("evidence_expired")
                else:
                    status = "passed"

                commit_matches = None
                if args.expected_commit:
                    commit_matches = item["commit"] == args.expected_commit
                    if not commit_matches:
                        gaps.append("commit_drift")

                cell = {
                    "target": target,
                    "environment": environment,
                    "status": status,
                    "age_hours": round(age_hours, 2),
                    "completed_at": item["completed_at"],
                    "tested_commit": item["commit"],
                    "commit_matches_expected": commit_matches,
                    "workflow_run_id": item["run_id"],
                    "workflow_run_attempt": item["run_attempt"],
                    "evidence_path": item["path"],
                    "evidence_sha256": item["evidence_sha256"],
                    "gaps": list(dict.fromkeys(gaps)),
                }

            counts[cell["status"]] += 1
            cells.append(cell)

    payload = {
        "schema_version": 1,
        "generated_at": current_time.isoformat().replace("+00:00", "Z"),
        "max_age_days": args.max_age_days,
        "expected_commit": args.expected_commit or None,
        "targets": targets,
        "environments": environments,
        "summary": {
            "total_cells": len(cells),
            "passed": counts["passed"],
            "failed": counts["failed"],
            "expired": counts["expired"],
            "not_run": counts["not_run"],
            "all_passed": counts["passed"] == len(cells),
            "discovered_evidence_files": discovered,
            "ignored_evidence_files": len(ignored),
        },
        "cells": cells,
        "ignored_evidence": ignored,
    }

    output_json = pathlib.Path(args.output_json)
    output_json.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")

    markdown = [
        "# Provider Validation Registry",
        "",
        f"Generated: {payload['generated_at']}",
        f"Evidence expiry: {args.max_age_days} day(s)",
        f"Expected commit: {args.expected_commit or 'not enforced'}",
        "",
        (
            f"Summary: passed={counts['passed']}, failed={counts['failed']}, "
            f"expired={counts['expired']}, not_run={counts['not_run']}"
        ),
        "",
        "| Target | Environment | Status | Age | Commit | Run | Gaps |",
        "| --- | --- | --- | ---: | --- | --- | --- |",
    ]
    for cell in cells:
        gaps = ", ".join(cell["gaps"]) if cell["gaps"] else "-"
        run = "-"
        if cell["workflow_run_id"]:
            run = f"{cell['workflow_run_id']}/{cell['workflow_run_attempt']}"
        markdown.append(
            "| {target} | {environment} | {status} | {age} | {commit} | {run} | {gaps} |".format(
                target=cell["target"],
                environment=cell["environment"],
                status=cell["status"],
                age=age_label(cell["age_hours"]),
                commit=short_commit(cell["tested_commit"] or ""),
                run=run,
                gaps=gaps.replace("|", "\\|"),
            )
        )

    if ignored:
        markdown.extend(["", "## Ignored evidence", ""])
        for item in ignored:
            markdown.append(f"- {item['path']}: {item['reason']}")

    pathlib.Path(args.output_markdown).write_text("\n".join(markdown) + "\n", encoding="utf-8")


def verify(args: argparse.Namespace) -> None:
    payload = json.loads(pathlib.Path(args.file).read_text(encoding="utf-8"))
    failures: list[str] = []

    if payload.get("schema_version") != 1:
        failures.append(f"schema_version: expected 1, got {payload.get('schema_version')!r}")

    cells = payload.get("cells")
    if not isinstance(cells, list):
        failures.append("cells: expected list")
        cells = []

    valid_status = {"not_run", "passed", "failed", "expired"}
    counts: Counter[str] = Counter()
    for cell in cells:
        if not isinstance(cell, dict):
            failures.append("cells: every entry must be an object")
            continue
        status = str(cell.get("status", ""))
        if status not in valid_status:
            failures.append(f"invalid cell status {status!r}")
            continue
        counts[status] += 1

        if args.require_current_commit and status == "passed" and cell.get("commit_matches_expected") is not True:
            failures.append(
                f"{cell.get('target')}/{cell.get('environment')}: passed evidence does not match expected commit"
            )

    summary = payload.get("summary", {})
    if not isinstance(summary, dict):
        failures.append("summary: expected object")
    else:
        for status in valid_status:
            if int(summary.get(status, -1)) != counts[status]:
                failures.append(
                    f"summary.{status}: expected {counts[status]}, got {summary.get(status)!r}"
                )
        if int(summary.get("total_cells", -1)) != len(cells):
            failures.append(
                f"summary.total_cells: expected {len(cells)}, got {summary.get('total_cells')!r}"
            )

    if args.expected_commit is not None and payload.get("expected_commit") != args.expected_commit:
        failures.append(
            f"expected_commit: expected {args.expected_commit!r}, got {payload.get('expected_commit')!r}"
        )

    if args.require_all_passed:
        not_passed = [
            f"{cell.get('target')}/{cell.get('environment')}={cell.get('status')}"
            for cell in cells
            if cell.get("status") != "passed"
        ]
        if not_passed:
            failures.append("not all registry cells passed: " + ", ".join(not_passed))

    if failures:
        for failure in failures:
            print(f"provider validation registry invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Provider validation registry PASSED: "
        f"cells={len(cells)} passed={counts['passed']} failed={counts['failed']} "
        f"expired={counts['expired']} not_run={counts['not_run']}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

build_parser = sub.add_parser("build")
build_parser.add_argument("--input-dir", required=True)
build_parser.add_argument("--output-json", required=True)
build_parser.add_argument("--output-markdown", required=True)
build_parser.add_argument("--max-age-days", type=int, default=30)
build_parser.add_argument("--expected-commit")
build_parser.add_argument("--targets", default=",".join(TARGETS))
build_parser.add_argument("--environments", default=",".join(DEFAULT_ENVIRONMENTS))
build_parser.add_argument("--now")
build_parser.set_defaults(func=build)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--expected-commit")
verify_parser.add_argument("--require-all-passed", action="store_true")
verify_parser.add_argument("--require-current-commit", action="store_true")
verify_parser.set_defaults(func=verify)

args = parser.parse_args()
try:
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"provider validation registry error: {exc}", file=sys.stderr)
    raise SystemExit(1)

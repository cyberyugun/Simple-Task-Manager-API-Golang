#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import pathlib
import sys
from datetime import datetime, timezone


def create(args: argparse.Namespace) -> None:
    payload = {
        "schema_version": 1,
        "status": "passed",
        "environment": "staging",
        "commit": args.commit,
        "image_digest": args.image_digest,
        "workflow_run_id": args.run_id,
        "source_ref": args.source_ref,
        "staging_url": args.staging_url,
        "completed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "checks": [
            "signed-image-verified",
            "staging-readiness",
            "migration",
            "rollout",
            "health",
            "readiness",
            "version",
            "k6-smoke",
        ],
    }
    pathlib.Path(args.output).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def verify(args: argparse.Namespace) -> None:
    payload = json.loads(pathlib.Path(args.file).read_text(encoding="utf-8"))
    required = {
        "status": "passed",
        "environment": "staging",
        "commit": args.commit,
        "image_digest": args.image_digest,
        "workflow_run_id": args.run_id,
    }
    failures = []
    for key, expected in required.items():
        actual = payload.get(key)
        if str(actual) != str(expected):
            failures.append(f"{key}: expected {expected!r}, got {actual!r}")

    checks = set(payload.get("checks", []))
    required_checks = {
        "signed-image-verified",
        "staging-readiness",
        "migration",
        "rollout",
        "health",
        "readiness",
        "version",
        "k6-smoke",
    }
    missing = sorted(required_checks - checks)
    if missing:
        failures.append("missing checks: " + ", ".join(missing))

    if failures:
        for failure in failures:
            print(f"promotion evidence invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "Promotion evidence PASSED: "
        f"commit={args.commit} digest={args.image_digest} run={args.run_id}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

create_parser = sub.add_parser("create")
create_parser.add_argument("--output", required=True)
create_parser.add_argument("--commit", required=True)
create_parser.add_argument("--image-digest", required=True)
create_parser.add_argument("--run-id", required=True)
create_parser.add_argument("--source-ref", required=True)
create_parser.add_argument("--staging-url", required=True)
create_parser.set_defaults(func=create)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--commit", required=True)
verify_parser.add_argument("--image-digest", required=True)
verify_parser.add_argument("--run-id", required=True)
verify_parser.set_defaults(func=verify)

args = parser.parse_args()
args.func(args)

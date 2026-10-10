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


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def read_object(path: pathlib.Path, label: str) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"{label} must be a JSON object")
    return payload


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_digest(payload: dict[str, Any], field: str) -> str:
    material = dict(payload)
    material.pop(field, None)
    encoded = json.dumps(
        material, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def validate_commit(value: str) -> str:
    commit = value.strip().lower()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("commit must be a full 40-character Git SHA")
    return commit


def validate_image_digest(value: str) -> str:
    digest = value.strip().lower()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("image_digest must be sha256:<64 lowercase hex characters>")
    return digest


def validate_run_id(value: str) -> str:
    run_id = value.strip()
    if not re.fullmatch(r"[0-9]+", run_id):
        raise ValueError("workflow run ID must be numeric")
    return run_id


def component(name: str, path: pathlib.Path, payload: dict[str, Any]) -> dict[str, Any]:
    return {
        "name": name,
        "file": path.name,
        "sha256": sha256_file(path),
        "status": payload.get("status"),
    }


def summarize_provider_policy(policy: dict[str, Any]) -> dict[str, Any]:
    decisions: list[dict[str, Any]] = []
    raw_decisions = policy.get("decisions")
    if isinstance(raw_decisions, list):
        for item in raw_decisions:
            if not isinstance(item, dict):
                continue
            operational = item.get("operational_evidence")
            if not isinstance(operational, dict):
                operational = {}
            decisions.append(
                {
                    "target": item.get("target"),
                    "registry_status": item.get("status"),
                    "policy_passed": item.get("policy_passed"),
                    "reasons": list(item.get("reasons") or []),
                    "operational_status": operational.get("status"),
                    "approval_status": operational.get("approval_status"),
                    "evidence_digest": operational.get("evidence_digest"),
                    "submitted_by": operational.get("submitted_by"),
                    "approved_by": operational.get("approved_by"),
                    "revoked_by": operational.get("revoked_by"),
                }
            )
    return {
        "mode": policy.get("mode"),
        "status": policy.get("status"),
        "required_targets": list(policy.get("required_targets") or []),
        "require_current_commit": bool(policy.get("require_current_commit", False)),
        "require_operational_evidence": bool(
            policy.get("require_operational_evidence", False)
        ),
        "require_operational_approval": bool(
            policy.get("require_operational_approval", False)
        ),
        "decisions": decisions,
    }


def summarize_expiry(expiry: dict[str, Any]) -> dict[str, Any]:
    rows: list[dict[str, Any]] = []
    raw_rows = expiry.get("rows")
    if isinstance(raw_rows, list):
        for item in raw_rows:
            if not isinstance(item, dict):
                continue
            rows.append(
                {
                    "target": item.get("target"),
                    "status": item.get("status"),
                    "days_remaining": item.get("days_remaining"),
                    "submitted_by": item.get("submitted_by"),
                    "approved_by": item.get("approved_by"),
                    "evidence_digest": item.get("evidence_digest"),
                }
            )
    return {
        "status": expiry.get("status"),
        "warning_days": expiry.get("warning_days"),
        "require_approval": bool(expiry.get("require_approval", False)),
        "rows": rows,
    }


def validate_release_component(
    payload: dict[str, Any], commit: str, image_digest: str
) -> list[str]:
    failures: list[str] = []
    if payload.get("status") != "success":
        failures.append("production release evidence status must be success")
    if str(payload.get("commit", "")).lower() != commit:
        failures.append("production release evidence commit does not match release commit")
    if str(payload.get("image_digest", "")).lower() != image_digest:
        failures.append("production release evidence image_digest does not match")
    return failures


def validate_staging_component(
    payload: dict[str, Any], commit: str, image_digest: str, run_id: str
) -> list[str]:
    failures: list[str] = []
    if payload.get("status") != "passed":
        failures.append("staging promotion evidence status must be passed")
    if payload.get("environment") != "staging":
        failures.append("staging promotion evidence environment must be staging")
    if str(payload.get("commit", "")).lower() != commit:
        failures.append("staging promotion evidence commit does not match release commit")
    if str(payload.get("image_digest", "")).lower() != image_digest:
        failures.append("staging promotion evidence image_digest does not match")
    if str(payload.get("workflow_run_id", "")) != run_id:
        failures.append("staging promotion evidence workflow_run_id does not match")
    return failures


def validate_registry_component(
    payload: dict[str, Any], commit: str
) -> list[str]:
    failures: list[str] = []
    if str(payload.get("expected_commit", "")).lower() != commit:
        failures.append("provider registry expected_commit does not match release commit")
    cells = payload.get("cells")
    if not isinstance(cells, list):
        failures.append("provider registry cells must be a list")
        return failures
    for item in cells:
        if not isinstance(item, dict):
            failures.append("provider registry cell must be an object")
            continue
        status = item.get("status")
        if status == "not_run":
            continue
        digest = str(item.get("evidence_sha256", "")).strip().lower()
        if not re.fullmatch(r"[0-9a-f]{64}", digest):
            failures.append(
                f"provider registry target {item.get('target')!r} is missing a valid evidence_sha256"
            )
    return failures


def validate_policy_component(
    payload: dict[str, Any], commit: str, gate_mode: str
) -> list[str]:
    failures: list[str] = []
    if payload.get("environment") != "production":
        failures.append("provider policy environment must be production")
    if str(payload.get("expected_commit", "")).lower() != commit:
        failures.append("provider policy expected_commit does not match release commit")
    if payload.get("mode") != gate_mode:
        failures.append(
            f"provider policy mode {payload.get('mode')!r} does not match gate mode {gate_mode!r}"
        )
    if payload.get("status") not in {"passed", "failed"}:
        failures.append(f"provider policy status is invalid: {payload.get('status')!r}")
    if gate_mode == "enforce" and payload.get("status") != "passed":
        failures.append("enforced provider policy must be passed")
    return failures


def create(args: argparse.Namespace) -> None:
    commit = validate_commit(args.commit)
    image_digest = validate_image_digest(args.image_digest)
    run_id = validate_run_id(args.run_id)
    run_attempt = validate_run_id(args.run_attempt)
    if args.provider_gate_mode not in {"off", "warn", "enforce"}:
        raise ValueError("provider-gate-mode must be off, warn, or enforce")
    if args.operational_approval_required and not args.operational_evidence_required:
        raise ValueError(
            "--operational-approval-required requires --operational-evidence-required"
        )

    release_path = pathlib.Path(args.release_evidence)
    staging_path = pathlib.Path(args.staging_evidence)
    release = read_object(release_path, "production release evidence")
    staging = read_object(staging_path, "staging promotion evidence")

    failures = validate_release_component(release, commit, image_digest)
    failures.extend(validate_staging_component(staging, commit, image_digest, run_id))

    components = [
        component("production_release", release_path, release),
        component("staging_promotion", staging_path, staging),
    ]

    provider_policy: dict[str, Any] | None = None
    provider_registry: dict[str, Any] | None = None
    provider_summary: dict[str, Any] = {
        "mode": args.provider_gate_mode,
        "status": "not_required" if args.provider_gate_mode == "off" else "missing",
        "required_targets": [],
        "require_current_commit": False,
        "require_operational_evidence": args.operational_evidence_required,
        "require_operational_approval": args.operational_approval_required,
        "decisions": [],
    }

    if args.provider_gate_mode != "off":
        if not args.provider_policy or not args.provider_registry:
            failures.append(
                "provider registry and policy are required when provider gate mode is active"
            )
        else:
            policy_path = pathlib.Path(args.provider_policy)
            registry_path = pathlib.Path(args.provider_registry)
            provider_policy = read_object(policy_path, "provider policy")
            provider_registry = read_object(registry_path, "provider registry")
            failures.extend(
                validate_policy_component(
                    provider_policy, commit, args.provider_gate_mode
                )
            )
            failures.extend(validate_registry_component(provider_registry, commit))
            provider_summary = summarize_provider_policy(provider_policy)
            components.append(
                component("provider_validation_registry", registry_path, provider_registry)
            )
            components.append(
                component("provider_validation_policy", policy_path, provider_policy)
            )

    expiry_summary: dict[str, Any] | None = None
    if args.operational_evidence_required:
        if not args.operational_expiry:
            failures.append(
                "operational expiry snapshot is required when operational evidence is required"
            )
        else:
            expiry_path = pathlib.Path(args.operational_expiry)
            expiry = read_object(expiry_path, "operational expiry snapshot")
            if expiry.get("environment") != "production":
                failures.append("operational expiry environment must be production")
            if (
                args.operational_approval_required
                and expiry.get("require_approval") is not True
            ):
                failures.append(
                    "operational expiry snapshot must require approval for this release"
                )
            expiry_summary = summarize_expiry(expiry)
            components.append(
                component("operational_expiry_snapshot", expiry_path, expiry)
            )

    if args.provider_gate_mode == "off" and (
        args.provider_policy or args.provider_registry
    ):
        failures.append(
            "provider policy/registry must not be supplied when provider gate mode is off"
        )

    if failures:
        for failure in list(dict.fromkeys(failures)):
            print(f"release attestation rejected: {failure}", file=sys.stderr)
        raise SystemExit(1)

    warning_reasons: list[str] = []
    if provider_summary.get("status") == "failed":
        warning_reasons.append("provider_policy_warn_mode_failed")
    if expiry_summary and expiry_summary.get("status") == "attention_required":
        warning_reasons.append("operational_evidence_attention_required")

    payload = {
        "schema_version": 1,
        "kind": "production_release_attestation",
        "status": "passed_with_warnings" if warning_reasons else "passed",
        "generated_at": utc_now(),
        "environment": "production",
        "commit": commit,
        "image_digest": image_digest,
        "workflow_run_id": run_id,
        "workflow_run_attempt": run_attempt,
        "source_ref": args.source_ref,
        "provider_gate_mode": args.provider_gate_mode,
        "operational_evidence_required": args.operational_evidence_required,
        "operational_approval_required": args.operational_approval_required,
        "components": components,
        "provider_assurance": provider_summary,
        "operational_expiry": expiry_summary,
        "warning_reasons": warning_reasons,
        "limitations": [
            "attestation_binds_retained_repository_artifacts_by_sha256",
            "external_evidence_content_is_not_independently_fetched_or_certified",
        ],
    }
    payload["root_digest"] = canonical_digest(payload, "root_digest")

    output_json = pathlib.Path(args.output_json)
    output_json.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")

    markdown = [
        "# Production Release Attestation",
        "",
        f"Status: {payload['status']}",
        f"Commit: `{commit}`",
        f"Image: `{image_digest}`",
        f"Workflow run: {run_id}/{run_attempt}",
        f"Root digest: `{payload['root_digest']}`",
        "",
        "## Bound components",
        "",
        "| Component | Status | SHA-256 |",
        "| --- | --- | --- |",
    ]
    for item in components:
        markdown.append(
            f"| {item['name']} | {item.get('status') or '-'} | `{item['sha256']}` |"
        )

    markdown.extend(
        [
            "",
            "## Provider assurance",
            "",
            f"Gate mode: {provider_summary.get('mode')}",
            f"Policy status: {provider_summary.get('status')}",
        ]
    )

    decisions = provider_summary.get("decisions") or []
    if decisions:
        markdown.extend(
            [
                "",
                "| Target | Registry | Operational | Approval | Policy | Reasons |",
                "| --- | --- | --- | --- | --- | --- |",
            ]
        )
        for item in decisions:
            reasons = ", ".join(item.get("reasons") or []) or "-"
            markdown.append(
                f"| {item.get('target') or '-'} | {item.get('registry_status') or '-'} | "
                f"{item.get('operational_status') or '-'} | "
                f"{item.get('approval_status') or '-'} | "
                f"{'passed' if item.get('policy_passed') else 'failed'} | {reasons} |"
            )
    else:
        markdown.append("")
        markdown.append("No provider targets are policy-critical for this release.")

    if expiry_summary is not None:
        markdown.extend(
            [
                "",
                "## Operational evidence expiry snapshot",
                "",
                f"Status: {expiry_summary.get('status')}",
                f"Warning window: {expiry_summary.get('warning_days')} days",
                "",
                "| Target | Status | Days remaining | Submitter | Approver |",
                "| --- | --- | ---: | --- | --- |",
            ]
        )
        for item in expiry_summary.get("rows") or []:
            days = item.get("days_remaining")
            days_text = "-" if days is None else str(days)
            markdown.append(
                f"| {item.get('target') or '-'} | {item.get('status') or '-'} | "
                f"{days_text} | {item.get('submitted_by') or '-'} | "
                f"{item.get('approved_by') or '-'} |"
            )

    if warning_reasons:
        markdown.extend(["", "## Warnings", ""])
        for warning in warning_reasons:
            markdown.append(f"- {warning}")

    markdown.extend(
        [
            "",
            "## Integrity model",
            "",
            "The root digest is a canonical SHA-256 over this manifest, which contains "
            "the SHA-256 digest of every bound component. Verification recomputes both "
            "the component hashes and the root digest.",
            "",
            "External operational references remain operator-attested; this bundle does "
            "not fetch or independently certify remote evidence content.",
        ]
    )
    pathlib.Path(args.output_markdown).write_text(
        "\n".join(markdown) + "\n", encoding="utf-8"
    )

    print(
        "Release attestation created: "
        f"status={payload['status']} commit={commit} root={payload['root_digest']}"
    )


def verify_manifest(
    manifest_path: pathlib.Path,
    component_dir: pathlib.Path,
    expected_commit: str | None = None,
    expected_image_digest: str | None = None,
    expected_run_id: str | None = None,
    require_passed: bool = False,
) -> list[str]:
    failures: list[str] = []
    payload = read_object(manifest_path, "release attestation")

    if payload.get("schema_version") != 1:
        failures.append(
            f"schema_version must be 1, got {payload.get('schema_version')!r}"
        )
    if payload.get("kind") != "production_release_attestation":
        failures.append("kind must be production_release_attestation")
    if payload.get("environment") != "production":
        failures.append("environment must be production")
    if payload.get("status") not in {"passed", "passed_with_warnings"}:
        failures.append(f"invalid attestation status {payload.get('status')!r}")
    if require_passed and payload.get("status") != "passed":
        failures.append("release attestation contains warnings")

    try:
        commit = validate_commit(str(payload.get("commit", "")))
    except ValueError as exc:
        failures.append(str(exc))
        commit = ""
    try:
        image_digest = validate_image_digest(str(payload.get("image_digest", "")))
    except ValueError as exc:
        failures.append(str(exc))
        image_digest = ""
    try:
        run_id = validate_run_id(str(payload.get("workflow_run_id", "")))
    except ValueError as exc:
        failures.append(str(exc))
        run_id = ""

    if expected_commit and commit != validate_commit(expected_commit):
        failures.append("attestation commit does not match expected commit")
    if expected_image_digest and image_digest != validate_image_digest(
        expected_image_digest
    ):
        failures.append("attestation image_digest does not match expected digest")
    if expected_run_id and run_id != validate_run_id(expected_run_id):
        failures.append("attestation workflow_run_id does not match expected run")

    root = str(payload.get("root_digest", "")).strip().lower()
    if not re.fullmatch(r"[0-9a-f]{64}", root):
        failures.append("root_digest must be a lowercase 64-character SHA-256 digest")
    elif root != canonical_digest(payload, "root_digest"):
        failures.append("root_digest does not match manifest content")

    components = payload.get("components")
    if not isinstance(components, list) or not components:
        failures.append("components must be a non-empty list")
        components = []

    seen_names: set[str] = set()
    loaded: dict[str, dict[str, Any]] = {}
    for item in components:
        if not isinstance(item, dict):
            failures.append("component entry must be an object")
            continue
        name = str(item.get("name", ""))
        filename = str(item.get("file", ""))
        expected_sha = str(item.get("sha256", "")).lower()
        if not name or name in seen_names:
            failures.append(f"component name is missing or duplicated: {name!r}")
            continue
        seen_names.add(name)
        if pathlib.Path(filename).name != filename or not filename:
            failures.append(f"component {name} has unsafe file name")
            continue
        path = component_dir / filename
        if not path.is_file():
            failures.append(f"component {name} file is missing: {filename}")
            continue
        actual_sha = sha256_file(path)
        if actual_sha != expected_sha:
            failures.append(
                f"component {name} SHA-256 mismatch: expected={expected_sha} actual={actual_sha}"
            )
            continue
        try:
            loaded[name] = read_object(path, name)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            failures.append(f"component {name} is invalid JSON: {exc}")

    if "production_release" not in loaded:
        failures.append("production_release component is required")
    elif commit and image_digest:
        failures.extend(
            validate_release_component(
                loaded["production_release"], commit, image_digest
            )
        )

    if "staging_promotion" not in loaded:
        failures.append("staging_promotion component is required")
    elif commit and image_digest and run_id:
        failures.extend(
            validate_staging_component(
                loaded["staging_promotion"], commit, image_digest, run_id
            )
        )

    gate_mode = str(payload.get("provider_gate_mode", ""))
    if gate_mode not in {"off", "warn", "enforce"}:
        failures.append("provider_gate_mode must be off, warn, or enforce")
    elif gate_mode != "off":
        policy = loaded.get("provider_validation_policy")
        registry = loaded.get("provider_validation_registry")
        if policy is None or registry is None:
            failures.append(
                "provider policy and registry components are required for active gate"
            )
        else:
            failures.extend(validate_policy_component(policy, commit, gate_mode))
            failures.extend(validate_registry_component(registry, commit))

    operational_required = payload.get("operational_evidence_required") is True
    approval_required = payload.get("operational_approval_required") is True
    if approval_required and not operational_required:
        failures.append(
            "operational_approval_required requires operational_evidence_required"
        )
    if operational_required:
        expiry = loaded.get("operational_expiry_snapshot")
        if expiry is None:
            failures.append(
                "operational_expiry_snapshot component is required for operational evidence"
            )
        else:
            if expiry.get("environment") != "production":
                failures.append("operational expiry snapshot environment must be production")
            if approval_required and expiry.get("require_approval") is not True:
                failures.append(
                    "operational expiry snapshot must require approval for this release"
                )

    return list(dict.fromkeys(failures))


def verify(args: argparse.Namespace) -> None:
    manifest = pathlib.Path(args.file)
    component_dir = (
        pathlib.Path(args.component_dir)
        if args.component_dir
        else manifest.resolve().parent
    )
    failures = verify_manifest(
        manifest,
        component_dir,
        args.commit,
        args.image_digest,
        args.run_id,
        args.require_passed,
    )
    if failures:
        for failure in failures:
            print(f"release attestation invalid: {failure}", file=sys.stderr)
        raise SystemExit(1)

    payload = read_object(manifest, "release attestation")
    print(
        "Release attestation PASSED: "
        f"commit={payload.get('commit')} image={payload.get('image_digest')} "
        f"root={payload.get('root_digest')}"
    )


def dashboard(args: argparse.Namespace) -> None:
    root = pathlib.Path(args.input_dir)
    manifests = list(root.rglob("release-attestation.json")) if root.exists() else []
    rows: list[dict[str, Any]] = []

    for path in manifests:
        try:
            payload = read_object(path, "release attestation")
            failures = verify_manifest(path, path.parent)
            generated_raw = str(payload.get("generated_at", ""))
            generated = datetime.fromisoformat(
                generated_raw.replace("Z", "+00:00")
            )
            if generated.tzinfo is None:
                raise ValueError("generated_at must include timezone")
            rows.append(
                {
                    "generated_at": generated.astimezone(timezone.utc),
                    "integrity": "valid" if not failures else "invalid",
                    "status": payload.get("status"),
                    "commit": payload.get("commit"),
                    "image_digest": payload.get("image_digest"),
                    "workflow_run_id": payload.get("workflow_run_id"),
                    "provider_gate_mode": payload.get("provider_gate_mode"),
                    "provider_policy_status": (
                        payload.get("provider_assurance", {}).get("status")
                        if isinstance(payload.get("provider_assurance"), dict)
                        else None
                    ),
                    "required_targets": (
                        payload.get("provider_assurance", {}).get("required_targets")
                        if isinstance(payload.get("provider_assurance"), dict)
                        else []
                    ),
                    "root_digest": payload.get("root_digest"),
                    "failures": failures,
                    "path": str(path),
                }
            )
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            rows.append(
                {
                    "generated_at": datetime.min.replace(tzinfo=timezone.utc),
                    "integrity": "invalid",
                    "status": "invalid",
                    "commit": None,
                    "image_digest": None,
                    "workflow_run_id": None,
                    "provider_gate_mode": None,
                    "provider_policy_status": None,
                    "required_targets": [],
                    "root_digest": None,
                    "failures": [str(exc)],
                    "path": str(path),
                }
            )

    rows.sort(key=lambda row: row["generated_at"], reverse=True)
    rows = rows[: args.max_releases]
    invalid_count = sum(1 for row in rows if row["integrity"] != "valid")

    serializable_rows: list[dict[str, Any]] = []
    for row in rows:
        item = dict(row)
        item["generated_at"] = (
            row["generated_at"].isoformat().replace("+00:00", "Z")
            if row["generated_at"].year > 1
            else None
        )
        serializable_rows.append(item)

    payload = {
        "schema_version": 1,
        "generated_at": utc_now(),
        "status": "attention_required" if invalid_count else "healthy",
        "release_count": len(rows),
        "invalid_count": invalid_count,
        "rows": serializable_rows,
    }
    pathlib.Path(args.output_json).write_text(
        json.dumps(payload, indent=2) + "\n", encoding="utf-8"
    )

    markdown = [
        "# Release Attestation Dashboard",
        "",
        f"Status: {payload['status']}",
        f"Releases inspected: {len(rows)}",
        f"Invalid bundles: {invalid_count}",
        "",
        "| Generated | Integrity | Release | Provider policy | Commit | Run | Root digest |",
        "| --- | --- | --- | --- | --- | --- | --- |",
    ]
    for row in serializable_rows:
        commit = (row.get("commit") or "-")[:12]
        digest = (row.get("root_digest") or "-")[:12]
        markdown.append(
            f"| {row.get('generated_at') or '-'} | {row.get('integrity')} | "
            f"{row.get('status') or '-'} | {row.get('provider_policy_status') or '-'} | "
            f"`{commit}` | {row.get('workflow_run_id') or '-'} | `{digest}` |"
        )
        if row.get("failures"):
            for failure in row["failures"]:
                markdown.append(f"|  | ↳ |  |  |  |  | {failure} |")

    pathlib.Path(args.output_markdown).write_text(
        "\n".join(markdown) + "\n", encoding="utf-8"
    )

    if args.require_valid and invalid_count:
        raise SystemExit(
            f"release attestation dashboard contains {invalid_count} invalid bundle(s)"
        )
    print(
        "Release attestation dashboard created: "
        f"releases={len(rows)} invalid={invalid_count}"
    )


parser = argparse.ArgumentParser()
sub = parser.add_subparsers(dest="command", required=True)

create_parser = sub.add_parser("create")
create_parser.add_argument("--output-json", required=True)
create_parser.add_argument("--output-markdown", required=True)
create_parser.add_argument("--commit", required=True)
create_parser.add_argument("--image-digest", required=True)
create_parser.add_argument("--run-id", required=True)
create_parser.add_argument("--run-attempt", required=True)
create_parser.add_argument("--source-ref", required=True)
create_parser.add_argument("--release-evidence", required=True)
create_parser.add_argument("--staging-evidence", required=True)
create_parser.add_argument(
    "--provider-gate-mode", choices=["off", "warn", "enforce"], default="off"
)
create_parser.add_argument("--provider-registry")
create_parser.add_argument("--provider-policy")
create_parser.add_argument("--operational-expiry")
create_parser.add_argument("--operational-evidence-required", action="store_true")
create_parser.add_argument("--operational-approval-required", action="store_true")
create_parser.set_defaults(func=create)

verify_parser = sub.add_parser("verify")
verify_parser.add_argument("--file", required=True)
verify_parser.add_argument("--component-dir")
verify_parser.add_argument("--commit")
verify_parser.add_argument("--image-digest")
verify_parser.add_argument("--run-id")
verify_parser.add_argument("--require-passed", action="store_true")
verify_parser.set_defaults(func=verify)

dashboard_parser = sub.add_parser("dashboard")
dashboard_parser.add_argument("--input-dir", required=True)
dashboard_parser.add_argument("--output-json", required=True)
dashboard_parser.add_argument("--output-markdown", required=True)
dashboard_parser.add_argument("--max-releases", type=int, default=50)
dashboard_parser.add_argument("--require-valid", action="store_true")
dashboard_parser.set_defaults(func=dashboard)

args = parser.parse_args()
try:
    if getattr(args, "max_releases", 1) <= 0 or getattr(args, "max_releases", 1) > 500:
        raise ValueError("max-releases must be between 1 and 500")
    args.func(args)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    print(f"release attestation error: {exc}", file=sys.stderr)
    raise SystemExit(1)

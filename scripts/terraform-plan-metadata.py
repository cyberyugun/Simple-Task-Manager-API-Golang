#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--plan-json", required=True)
parser.add_argument("--plan-bin", required=True)
parser.add_argument("--lock-file", required=True)
parser.add_argument("--provider", required=True)
parser.add_argument("--commit", required=True)
parser.add_argument("--run-id", required=True)
parser.add_argument("--terraform-version", required=True)
parser.add_argument("--allow-destroy", action="store_true")
parser.add_argument("--output", required=True)
args = parser.parse_args()

plan = json.loads(Path(args.plan_json).read_text(encoding="utf-8"))
counts = {
    "create": 0,
    "update": 0,
    "delete": 0,
    "replace": 0,
    "no_op": 0,
    "read": 0,
}

destructive = []

for change in plan.get("resource_changes", []):
    actions = change.get("change", {}).get("actions", [])
    address = change.get("address", "<unknown>")

    if actions == ["create"]:
        counts["create"] += 1
    elif actions == ["update"]:
        counts["update"] += 1
    elif actions == ["delete"]:
        counts["delete"] += 1
        destructive.append(address)
    elif "delete" in actions and "create" in actions:
        counts["replace"] += 1
        destructive.append(address)
    elif actions == ["no-op"]:
        counts["no_op"] += 1
    elif actions == ["read"]:
        counts["read"] += 1

def sha256(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

metadata = {
    "provider": args.provider,
    "commit": args.commit,
    "run_id": str(args.run_id),
    "terraform_version": args.terraform_version,
    "plan_sha256": sha256(args.plan_bin),
    "lock_sha256": sha256(args.lock_file),
    "counts": counts,
    "destructive_resources": destructive,
    "destructive_change_count": len(destructive),
}

Path(args.output).write_text(
    json.dumps(metadata, indent=2, sort_keys=True) + "\n",
    encoding="utf-8",
)

print(json.dumps(metadata["counts"], sort_keys=True))
if destructive:
    print("Destructive changes:")
    for address in destructive:
        print(f" - {address}")
    if not args.allow_destroy:
        raise SystemExit(
            "Terraform plan contains delete/replacement actions. "
            "Re-run a manual plan with allow_destroy=true only after explicit review."
        )

#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--metadata", required=True)
parser.add_argument("--plan-bin", required=True)
parser.add_argument("--lock-file", required=True)
parser.add_argument("--expected-provider", required=True)
parser.add_argument("--expected-commit", required=True)
parser.add_argument("--expected-run-id", required=True)
parser.add_argument("--confirm-destroy", action="store_true")
args = parser.parse_args()

metadata = json.loads(Path(args.metadata).read_text(encoding="utf-8"))

def sha256(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

checks = {
    "provider": (metadata.get("provider"), args.expected_provider),
    "commit": (metadata.get("commit"), args.expected_commit),
    "run_id": (str(metadata.get("run_id")), str(args.expected_run_id)),
    "plan_sha256": (metadata.get("plan_sha256"), sha256(args.plan_bin)),
    "lock_sha256": (metadata.get("lock_sha256"), sha256(args.lock_file)),
}

for name, (actual, expected) in checks.items():
    if actual != expected:
        raise SystemExit(f"{name} mismatch: metadata={actual!r} expected={expected!r}")

destructive_count = int(metadata.get("destructive_change_count", 0))
if destructive_count > 0 and not args.confirm_destroy:
    raise SystemExit(
        f"plan contains {destructive_count} destructive change(s); "
        "confirm_destroy=true is required for apply"
    )

print("Terraform plan artifact verified.")
print(json.dumps(metadata.get("counts", {}), sort_keys=True))

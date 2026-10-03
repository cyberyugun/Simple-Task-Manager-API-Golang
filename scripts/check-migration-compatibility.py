#!/usr/bin/env python3
from __future__ import annotations

import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "migrations")
if not root.is_dir():
    raise SystemExit(f"migration directory not found: {root}")

forbidden = [
    (re.compile(r"\bDROP\s+TABLE\b", re.IGNORECASE), "DROP TABLE"),
    (re.compile(r"\bDROP\s+COLUMN\b", re.IGNORECASE), "DROP COLUMN"),
    (re.compile(r"\bTRUNCATE\b", re.IGNORECASE), "TRUNCATE"),
    (re.compile(r"\bALTER\s+TABLE\b[\s\S]*?\bRENAME\b", re.IGNORECASE), "ALTER TABLE ... RENAME"),
    (re.compile(r"\bRENAME\s+COLUMN\b", re.IGNORECASE), "RENAME COLUMN"),
    (re.compile(r"\bALTER\s+COLUMN\b[\s\S]*?\bTYPE\b", re.IGNORECASE), "ALTER COLUMN ... TYPE"),
]

def strip_comments(sql: str) -> str:
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.DOTALL)
    sql = re.sub(r"--[^\n]*", " ", sql)
    return sql

failures: list[str] = []
files = sorted(root.glob("*.sql"))
if not files:
    failures.append("no SQL migration files found")

for path in files:
    sql = strip_comments(path.read_text(encoding="utf-8"))
    for pattern, label in forbidden:
        if pattern.search(sql):
            failures.append(
                f"{path}: destructive/incompatible operation detected: {label}; "
                "use an expand/contract migration across separate releases"
            )
    if re.search(r"\bCREATE\s+(UNIQUE\s+)?INDEX\s+CONCURRENTLY\b", sql, re.IGNORECASE):
        failures.append(
            f"{path}: CREATE INDEX CONCURRENTLY is incompatible with the current "
            "transaction-per-migration runner"
        )

if failures:
    print("Migration compatibility gate FAILED:", file=sys.stderr)
    for failure in failures:
        print(f"- {failure}", file=sys.stderr)
    raise SystemExit(1)

print(f"Migration compatibility gate PASSED for {len(files)} migration file(s).")

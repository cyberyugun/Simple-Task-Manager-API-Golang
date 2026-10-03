#!/usr/bin/env bash
set -euo pipefail

: "${TF_BACKEND_CONFIG_B64:?TF_BACKEND_CONFIG_B64 is required}"
: "${TF_VARS_B64:?TF_VARS_B64 is required}"

printf '%s' "$TF_BACKEND_CONFIG_B64" | base64 --decode > /tmp/backend.hcl
printf '%s' "$TF_VARS_B64" | base64 --decode > /tmp/terraform.tfvars

chmod 600 /tmp/backend.hcl /tmp/terraform.tfvars

grep -Eq '[^[:space:]]' /tmp/backend.hcl
grep -Eq '[^[:space:]]' /tmp/terraform.tfvars

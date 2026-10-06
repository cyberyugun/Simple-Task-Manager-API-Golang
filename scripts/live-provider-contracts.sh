#!/usr/bin/env bash
set -euo pipefail

target="${LIVE_PROVIDER_CONTRACT_TARGET:-${1:-}}"
if [[ -z "$target" ]]; then
  echo "LIVE_PROVIDER_CONTRACT_TARGET is required" >&2
  exit 2
fi

case "$target" in
  storage_s3|storage_azure|storage_gcs)
    ;;
  scanner)
    if [[ -z "${LIVE_PROVIDER_STORAGE_TARGET:-}" ]]; then
      echo "LIVE_PROVIDER_STORAGE_TARGET is required when target=scanner" >&2
      exit 2
    fi
    case "$LIVE_PROVIDER_STORAGE_TARGET" in
      storage_s3|storage_azure|storage_gcs)
        ;;
      *)
        echo "LIVE_PROVIDER_STORAGE_TARGET must be storage_s3, storage_azure, or storage_gcs" >&2
        exit 2
        ;;
    esac
    ;;
  *)
    echo "unsupported live provider contract target: $target" >&2
    exit 2
    ;;
esac

export LIVE_PROVIDER_CONTRACTS=true
export LIVE_PROVIDER_CONTRACT_TARGET="$target"

echo "Running live provider contract target=$LIVE_PROVIDER_CONTRACT_TARGET"
if [[ "$target" == "scanner" ]]; then
  echo "Scanner fixture storage target=$LIVE_PROVIDER_STORAGE_TARGET"
fi

go test -count=1 -run '^TestLiveAttachmentProviderContract$' -v ./internal/service

#!/usr/bin/env bash
set -euo pipefail

target="${LIVE_PROVIDER_CONTRACT_TARGET:-${1:-}}"
if [[ -z "$target" ]]; then
  echo "LIVE_PROVIDER_CONTRACT_TARGET is required" >&2
  exit 2
fi

case "$target" in
  storage_s3|storage_azure|storage_gcs)
    test_name="TestLiveAttachmentProviderContract"
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
    test_name="TestLiveAttachmentProviderContract"
    ;;
  secret_vault)
    test_name="TestLiveSecretProviderContract"
    ;;
  secret_aws)
    export CONNECTOR_AWS_SECRETS_MANAGER_ENABLED=true
    test_name="TestLiveSecretProviderContract"
    ;;
  secret_azure)
    export CONNECTOR_AZURE_KEY_VAULT_ENABLED=true
    test_name="TestLiveSecretProviderContract"
    ;;
  secret_gcp)
    export CONNECTOR_GCP_SECRET_MANAGER_ENABLED=true
    test_name="TestLiveSecretProviderContract"
    ;;
  warehouse_bigquery)
    export DATA_PLATFORM_BIGQUERY_NATIVE=true
    test_name="TestLiveWarehouseProviderContract"
    ;;
  warehouse_snowflake)
    export DATA_PLATFORM_SNOWFLAKE_NATIVE=true
    test_name="TestLiveWarehouseProviderContract"
    ;;
  warehouse_redshift)
    export DATA_PLATFORM_REDSHIFT_NATIVE=true
    test_name="TestLiveWarehouseProviderContract"
    ;;
  warehouse_databricks)
    export DATA_PLATFORM_DATABRICKS_NATIVE=true
    test_name="TestLiveWarehouseProviderContract"
    ;;
  ai_remote)
    export AI_REMOTE_PROVIDER_ENABLED=true
    test_name="TestLiveRemoteAIProviderContract"
    ;;
  region_automation)
    export REGION_AUTOMATION_ENABLED=true
    test_name="TestLiveRegionAutomationProviderContract"
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

go test -count=1 -run "^${test_name}$" -v ./internal/service

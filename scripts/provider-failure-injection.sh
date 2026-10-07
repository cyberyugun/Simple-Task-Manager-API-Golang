#!/usr/bin/env bash
set -euo pipefail

target="${LIVE_PROVIDER_CONTRACT_TARGET:-${1:-}}"
if [[ -z "$target" ]]; then
  echo "LIVE_PROVIDER_CONTRACT_TARGET is required" >&2
  exit 2
fi

case "$target" in
  storage_s3)
    tests='TestS3ObjectStoreVerifyRejectsMetadataMismatch|TestS3ObjectStoreRejectsUnsafeConfigAndKeys'
    ;;
  storage_azure)
    tests='TestAzureBlobRejectsUnsafeConfiguration'
    ;;
  storage_gcs)
    tests='TestGCSObjectStoreVerifyRejectsMetadataMismatch|TestGCSObjectStoreRejectsUnsafeConfiguration'
    ;;
  scanner)
    tests='TestNewAttachmentScannerRejectsInsecureEndpointByDefault|TestHTTPAttachmentScannerInfectedAndFailureAreFailClosed|TestHTTPAttachmentScannerDoesNotFollowRedirects'
    ;;
  secret_vault)
    tests='TestHashiCorpVaultSecretStoreRejectsUnsafeConfiguration|TestConnectorRejectsUnconfiguredExternalBackendRotation'
    ;;
  secret_aws)
    tests='TestAWSSecretsManagerRejectsUnsafeOrIncompleteConfiguration'
    ;;
  secret_azure)
    tests='TestAzureKeyVaultRejectsUnsafeConfiguration'
    ;;
  secret_gcp)
    tests='TestGCPSecretManagerRejectsUnsafeConfiguration'
    ;;
  warehouse_bigquery)
    tests='TestBigQueryWarehouseAdapterRetriesTransientInsert|TestNativeBigQueryFailureDoesNotAdvanceCheckpoint|TestBigQueryWarehouseAdapterRejectsUnsafeConfiguration'
    ;;
  warehouse_snowflake)
    tests='TestSnowflakeWarehouseAdapterRetriesTransientMerge|TestNativeSnowflakeFailureDoesNotAdvanceCheckpoint|TestSnowflakeWarehouseAdapterRejectsUnsafeConfiguration'
    ;;
  warehouse_redshift)
    tests='TestRedshiftWarehouseAdapterRetriesTransientExecute|TestNativeRedshiftFailureDoesNotAdvanceCheckpoint|TestRedshiftWarehouseAdapterRejectsUnsafeConfiguration'
    ;;
  warehouse_databricks)
    tests='TestDatabricksWarehouseAdapterRetriesTransientMerge|TestNativeDatabricksFailureDoesNotAdvanceCheckpoint|TestDatabricksWarehouseAdapterRejectsUnsafeConfiguration'
    ;;
  ai_remote)
    tests='TestRemoteStructuredAIProviderRetriesWithStableIdempotencyKey|TestRemoteStructuredAIProviderRejectsMalformedStructuredResult|TestRemoteStructuredAIProviderRejectsUnsafeConfiguration'
    ;;
  region_automation)
    tests='TestRegionAutomationWebhookPlannerRetriesWithStableIdempotencyAndSignature|TestRegionAutomationWebhookPlannerRejectsUnsafeConfigurationAndUnapprovedMigration|TestGlobalRegionPlanningFailureKeepsMigrationPending'
    ;;
  *)
    echo "unsupported provider failure-injection target: $target" >&2
    exit 2
    ;;
esac

echo "Running hermetic provider failure-injection target=$target"
go test -count=1 -run "^($tests)$" -v ./internal/service

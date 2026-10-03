#!/usr/bin/env bash
set -Eeuo pipefail

KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
KUBE_NAMESPACE="${KUBE_NAMESPACE:-task-manager}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:?IMAGE_REPOSITORY is required}"
IMAGE_DIGEST="${IMAGE_DIGEST:?IMAGE_DIGEST is required}"
INGRESS_HOST="${INGRESS_HOST:?INGRESS_HOST is required}"
CERT_MANAGER_EMAIL="${CERT_MANAGER_EMAIL:?CERT_MANAGER_EMAIL is required}"
EXPECTED_COMMIT="${EXPECTED_COMMIT:?EXPECTED_COMMIT is required}"
CANARY_WEIGHT="${CANARY_WEIGHT:-10}"
CANARY_OBSERVATION_SECONDS="${CANARY_OBSERVATION_SECONDS:-60}"
EVIDENCE_PATH="${EVIDENCE_PATH:-release-evidence.json}"

stable_existed=false
worker_existed=false
promotion_started=false
previous_image=""
previous_worker_image=""
port_forward_pid=""
started_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
tmpdir="$(mktemp -d)"

cleanup_tmp() {
  rm -rf "$tmpdir"
}

write_evidence() {
  local status="$1"
  local finished_at
  finished_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  python3 - "$EVIDENCE_PATH" "$status" "$started_at" "$finished_at" "$EXPECTED_COMMIT" "$IMAGE_DIGEST" "$previous_image" "$CANARY_WEIGHT" "$CANARY_OBSERVATION_SECONDS" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
payload = {
    "status": sys.argv[2],
    "started_at": sys.argv[3],
    "finished_at": sys.argv[4],
    "commit": sys.argv[5],
    "image_digest": sys.argv[6],
    "previous_image": sys.argv[7] or None,
    "canary_weight_percent": int(sys.argv[8]),
    "canary_observation_seconds": int(sys.argv[9]),
    "migration_policy": "expand-contract",
    "canary_header": "X-Canary: always",
}
path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
PY
}

cleanup_canary() {
  "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete ingress task-api-canary --ignore-not-found --wait=false >/dev/null 2>&1 || true
  "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete service task-api-canary --ignore-not-found >/dev/null 2>&1 || true
  "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete deployment task-api-canary --ignore-not-found --wait=false >/dev/null 2>&1 || true
}

rollback_on_error() {
  local exit_code=$?
  trap - ERR
  set +e

  if [ -n "$port_forward_pid" ]; then
    kill "$port_forward_pid" 2>/dev/null || true
  fi

  echo "Progressive release failed; cleaning up canary resources." >&2
  cleanup_canary

  if [ "$promotion_started" = "true" ]; then
    if [ "$stable_existed" = "true" ] && [ -n "$previous_image" ]; then
      echo "Rolling stable deployment back to $previous_image" >&2
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" set image deployment/task-api "api=$previous_image"
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api --timeout=5m
    else
      echo "No previous stable deployment existed; removing failed bootstrap deployment." >&2
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete deployment task-api --ignore-not-found --wait=true
    fi

    if [ "$worker_existed" = "true" ] && [ -n "$previous_worker_image" ]; then
      echo "Rolling worker deployment back to $previous_worker_image" >&2
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" set image deployment/task-worker "worker=$previous_worker_image"
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-worker --timeout=5m
    else
      "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete deployment task-worker --ignore-not-found --wait=true
    fi
  fi

  write_evidence "failed"
  cleanup_tmp
  exit "$exit_code"
}
trap rollback_on_error ERR

bash scripts/release-policy-check.sh

if "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-api >/dev/null 2>&1; then
  stable_existed=true
  previous_image="$("$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-api -o jsonpath='{.spec.template.spec.containers[?(@.name=="api")].image}')"
  echo "Previous stable image: $previous_image"
else
  echo "No existing stable deployment; this release will bootstrap it after canary verification."
fi

if "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-worker >/dev/null 2>&1; then
  worker_existed=true
  previous_worker_image="$("$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-worker -o jsonpath='{.spec.template.spec.containers[?(@.name=="worker")].image}')"
  echo "Previous worker image: $previous_worker_image"
fi

echo "Applying production runtime resources without changing the stable Deployment."
cp deploy/k8s/overlays/production/ingress.yaml "$tmpdir/ingress.yaml"
cp deploy/k8s/overlays/production/clusterissuer.yaml "$tmpdir/clusterissuer.yaml"
sed -i "s/task-api.example.com/$INGRESS_HOST/g" "$tmpdir/ingress.yaml"
sed -i "s/platform@example.com/$CERT_MANAGER_EMAIL/g" "$tmpdir/clusterissuer.yaml"

"$KUBECTL_BIN" apply -f deploy/k8s/base/serviceaccount.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/configmap.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/service.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/worker-service.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/hpa.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/pdb.yaml
"$KUBECTL_BIN" apply -f deploy/k8s/base/networkpolicy.yaml
"$KUBECTL_BIN" apply -f "$tmpdir/clusterissuer.yaml"
"$KUBECTL_BIN" apply -f "$tmpdir/ingress.yaml"
"$KUBECTL_BIN" apply -k deploy/k8s/observability

echo "Running forward-compatible database migrations from signed digest."
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" delete job task-api-migrate --ignore-not-found --wait=true
sed "s|image: task-api:latest|image: $IMAGE_REPOSITORY@$IMAGE_DIGEST|"   deploy/k8s/base/migration-job.yaml > "$tmpdir/migration-job.yaml"
"$KUBECTL_BIN" apply -f "$tmpdir/migration-job.yaml"

if ! "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" wait   --for=condition=complete job/task-api-migrate   --timeout=5m; then
  "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" logs job/task-api-migrate --all-containers=true || true
  false
fi

echo "Deploying canary at ${CANARY_WEIGHT}% traffic."
cp deploy/k8s/progressive/canary-deployment.yaml "$tmpdir/canary-deployment.yaml"
cp deploy/k8s/progressive/canary-ingress.yaml "$tmpdir/canary-ingress.yaml"
sed -i "s|image: task-api:latest|image: $IMAGE_REPOSITORY@$IMAGE_DIGEST|" "$tmpdir/canary-deployment.yaml"
sed -i "s/task-api.example.com/$INGRESS_HOST/g" "$tmpdir/canary-ingress.yaml"
sed -i "s/canary-weight: \"10\"/canary-weight: \"$CANARY_WEIGHT\"/" "$tmpdir/canary-ingress.yaml"

"$KUBECTL_BIN" apply -f deploy/k8s/progressive/canary-service.yaml
"$KUBECTL_BIN" apply -f "$tmpdir/canary-deployment.yaml"
"$KUBECTL_BIN" apply -f "$tmpdir/canary-ingress.yaml"
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api-canary --timeout=3m

echo "Verifying canary directly through its Service."
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" port-forward service/task-api-canary 18081:80 >"$tmpdir/canary-port-forward.log" 2>&1 &
port_forward_pid=$!

for attempt in $(seq 1 20); do
  if curl --fail --silent http://127.0.0.1:18081/version >"$tmpdir/canary-version.json"; then
    break
  fi
  sleep 1
done
curl --fail --silent http://127.0.0.1:18081/health >/dev/null
curl --fail --silent http://127.0.0.1:18081/ready >/dev/null
python3 - "$tmpdir/canary-version.json" "$EXPECTED_COMMIT" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    actual = json.load(handle)["data"]["commit"]
if actual != sys.argv[2]:
    raise SystemExit(f"canary commit {actual!r} does not match {sys.argv[2]!r}")
PY
kill "$port_forward_pid" 2>/dev/null || true
wait "$port_forward_pid" 2>/dev/null || true
port_forward_pid=""

echo "Verifying forced canary routing through public NGINX ingress."
public_version="$tmpdir/public-canary-version.json"
for attempt in $(seq 1 20); do
  if curl --fail --silent --show-error     --connect-timeout 5 --max-time 10     -H "X-Canary: always"     "https://$INGRESS_HOST/version" >"$public_version"; then
    if python3 - "$public_version" "$EXPECTED_COMMIT" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    actual = json.load(handle)["data"]["commit"]
raise SystemExit(0 if actual == sys.argv[2] else 1)
PY
    then
      break
    fi
  fi
  if [ "$attempt" -eq 20 ]; then
    echo "Forced public canary route did not serve expected commit." >&2
    false
  fi
  sleep 3
done

echo "Observing canary for $CANARY_OBSERVATION_SECONDS seconds."
deadline=$((SECONDS + CANARY_OBSERVATION_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  curl --fail --silent --show-error --connect-timeout 5 --max-time 10     -H "X-Canary: always" "https://$INGRESS_HOST/health" >/dev/null
  curl --fail --silent --show-error --connect-timeout 5 --max-time 10     -H "X-Canary: always" "https://$INGRESS_HOST/ready" >/dev/null
  curl --fail --silent --show-error --connect-timeout 5 --max-time 10     -H "X-Canary: always" "https://$INGRESS_HOST/version" >"$public_version"
  python3 - "$public_version" "$EXPECTED_COMMIT" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    actual = json.load(handle)["data"]["commit"]
if actual != sys.argv[2]:
    raise SystemExit(f"canary observation served unexpected commit {actual!r}")
PY
  sleep 5
done

echo "Canary passed; promoting immutable digest to stable Deployment."
promotion_started=true
sed "s|image: task-api:latest|image: $IMAGE_REPOSITORY@$IMAGE_DIGEST|" deploy/k8s/base/deployment.yaml > "$tmpdir/stable-deployment.yaml"
sed "s|image: task-api:latest|image: $IMAGE_REPOSITORY@$IMAGE_DIGEST|" deploy/k8s/base/worker-deployment.yaml > "$tmpdir/worker-deployment.yaml"
"$KUBECTL_BIN" apply -f "$tmpdir/stable-deployment.yaml"
"$KUBECTL_BIN" apply -f "$tmpdir/worker-deployment.yaml"
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api --timeout=5m
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-worker --timeout=5m

echo "Verifying stable public route."
for attempt in $(seq 1 30); do
  if curl --fail --silent --show-error     --connect-timeout 5 --max-time 10     "https://$INGRESS_HOST/version" >"$tmpdir/stable-version.json"; then
    if python3 - "$tmpdir/stable-version.json" "$EXPECTED_COMMIT" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    actual = json.load(handle)["data"]["commit"]
raise SystemExit(0 if actual == sys.argv[2] else 1)
PY
    then
      break
    fi
  fi
  if [ "$attempt" -eq 30 ]; then
    echo "Stable route did not converge to the expected commit." >&2
    false
  fi
  sleep 2
done

"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-api -o json >"$tmpdir/stable-deployment.json"
python3 - "$tmpdir/stable-deployment.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    deployment = json.load(handle)

spec_replicas = deployment.get("spec", {}).get("replicas", 0)
status = deployment.get("status", {})
available = status.get("availableReplicas", 0)
unavailable = status.get("unavailableReplicas", 0)

if unavailable:
    raise SystemExit(f"stable deployment still has {unavailable} unavailable replicas")
if available < spec_replicas:
    raise SystemExit(f"only {available}/{spec_replicas} stable replicas are available")
print(f"Zero-downtime verification PASSED: {available}/{spec_replicas} replicas available.")
PY

cleanup_canary
write_evidence "success"
cleanup_tmp
trap - ERR

echo "Progressive release PASSED."

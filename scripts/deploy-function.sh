#!/usr/bin/env bash
# deploy-function.sh — ship the Go function through the Appwrite REST API.
#
# CI-only by design: no SSH, no docker on the VPS, no manual upload. Mirrors
# scripts/deploy-appwrite.sh from the sibling canaries.
#
# Required env:
#   APPWRITE_ENDPOINT     https://appwrite.danielvm.net/v1
#   APPWRITE_PROJECT_ID   project that owns the function
#   APPWRITE_API_KEY      key with functions.write
#   APPWRITE_FUNCTION_ID  function id (e.g. todo-api-mysql)
# Optional:
#   ENTRYPOINT            default main.go
set -euo pipefail

: "${APPWRITE_ENDPOINT:?}"
: "${APPWRITE_PROJECT_ID:?}"
: "${APPWRITE_API_KEY:?}"
: "${APPWRITE_FUNCTION_ID:?}"
ENTRYPOINT=${ENTRYPOINT:-main.go}

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCHIVE="$(mktemp -t fn-deploy.XXXXXX.tar.gz)"
trap 'rm -f "$ARCHIVE"' EXIT

# Source only: the Go runtime compiles inside the build container.
tar -czf "$ARCHIVE" -C "$ROOT" \
  --exclude='./.git' --exclude='./web' --exclude='./specs' --exclude='./docs' \
  --exclude='./bin' --exclude='./node_modules' \
  go.mod go.sum main.go VERSION pkg

auth=(-H "X-Appwrite-Project: ${APPWRITE_PROJECT_ID}" -H "X-Appwrite-Key: ${APPWRITE_API_KEY}")
RESP="$(curl -sS -X POST "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/deployments" "${auth[@]}" \
  -F "entrypoint=${ENTRYPOINT}" -F "activate=true" \
  -F "code=@${ARCHIVE};type=application/gzip;filename=code.tar.gz")"

DEPLOY_ID="$(python3 -c 'import json,sys;print(json.load(sys.stdin).get("$id",""))' <<<"$RESP")"
[ -n "$DEPLOY_ID" ] || { echo "create deployment failed: $RESP" >&2; exit 1; }
echo "deployment ${DEPLOY_ID} created"

for _ in $(seq 1 120); do
  STATUS="$(curl -sS "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/deployments/${DEPLOY_ID}" "${auth[@]}" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))')"
  case "$STATUS" in
    ready)   echo "deployment ready"; echo "$DEPLOY_ID" > /tmp/function-deployment-id; exit 0;;
    failed)  echo "deployment failed" >&2; exit 1;;
    *)       echo "  status: ${STATUS:-?}"; sleep 10;;
  esac
done
echo "timed out waiting for the deployment" >&2
exit 1

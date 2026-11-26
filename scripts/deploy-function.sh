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

# Source only: the Go runtime compiles inside the build container. The module
# must stay named `openruntimes/handler` — the runtime builds it with
# `replace openruntimes/handler => /usr/local/build`.
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

READY=0
for _ in $(seq 1 120); do
  STATUS="$(curl -sS "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/deployments/${DEPLOY_ID}" "${auth[@]}" \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("status",""))')"
  case "$STATUS" in
    ready)  echo "deployment ready"; READY=1; break;;
    failed) echo "deployment failed" >&2
            curl -sS "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/deployments/${DEPLOY_ID}" "${auth[@]}" \
              | python3 -c 'import json,sys;print(json.load(sys.stdin).get("buildLogs",""))' | tail -40 >&2
            exit 1;;
    *)      echo "  status: ${STATUS:-?}"; sleep 10;;
  esac
done
[ "$READY" = 1 ] || { echo "timed out waiting for the deployment" >&2; exit 1; }

# Stamp the running version so GET /health reports the tag, not "dev".
VERSION="$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)"
if [ -n "${VERSION:-}" ]; then
  payload="$(python3 -c 'import json,sys;print(json.dumps({"variableId":"app-version","key":"APP_VERSION","value":sys.argv[1]}))' "$VERSION")"
  curl -sS -o /dev/null -X POST "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/variables" "${auth[@]}" \
    -H 'Content-Type: application/json' -d "$payload" || true
  echo "APP_VERSION=${VERSION}"
fi

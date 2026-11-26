#!/usr/bin/env bash
# deploy-site.sh — ship web/ as a static Appwrite Site (optional step in CI).
# Requires APPWRITE_ENDPOINT, APPWRITE_PROJECT_ID, APPWRITE_API_KEY, APPWRITE_SITE_ID.
# The browser config is generated from repository variables, never committed.
set -euo pipefail
: "${APPWRITE_ENDPOINT:?}"; : "${APPWRITE_PROJECT_ID:?}"; : "${APPWRITE_API_KEY:?}"; : "${APPWRITE_SITE_ID:?}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cp "$ROOT"/web/index.html "$ROOT"/web/app.js "$ROOT"/web/styles.css "$WORK"/
cat > "$WORK/config.js" <<CFG
window.__TODO_CONFIG__ = {
  endpoint: "${APPWRITE_ENDPOINT%/}",
  projectId: "${APPWRITE_PROJECT_ID}",
  functionId: "${APPWRITE_FUNCTION_ID:-todo-api-mysql}",
  appwriteSdkUrl: "https://esm.sh/appwrite@22",
};
CFG

ARCHIVE="$(mktemp -t site-deploy.XXXXXX.tar.gz)"
tar -czf "$ARCHIVE" -C "$WORK" .

auth=(-H "X-Appwrite-Project: ${APPWRITE_PROJECT_ID}" -H "X-Appwrite-Key: ${APPWRITE_API_KEY}")
DEPLOY="$(curl -sS -X POST "${APPWRITE_ENDPOINT%/}/sites/${APPWRITE_SITE_ID}/deployments" "${auth[@]}" \
  -F "entrypoint=" -F "activate=true" \
  -F "code=@${ARCHIVE};type=application/gzip;filename=code.tar.gz")"
DEPLOY_ID="$(python3 -c 'import json,sys;print(json.load(sys.stdin).get("$id",""))' <<<"$DEPLOY")"
[ -n "$DEPLOY_ID" ] || { echo "site deployment failed: $DEPLOY" >&2; exit 1; }

for _ in $(seq 1 60); do
  STATUS="$(curl -sS "${APPWRITE_ENDPOINT%/}/sites/${APPWRITE_SITE_ID}/deployments/${DEPLOY_ID}" "${auth[@]}" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))')"
  [ "$STATUS" = "ready" ] && { echo "site ready"; exit 0; }
  [ "$STATUS" = "failed" ] && { echo "site deployment failed" >&2; exit 1; }
  sleep 5
done
exit 1

#!/usr/bin/env bash
# execute-function.sh — run the deployed function once (CI → migrate/smoke).
#
#   PATH_ QUERY METHOD BODY HEADERS...  via env, defaults below.
# Requires APPWRITE_ENDPOINT, APPWRITE_PROJECT_ID, APPWRITE_API_KEY, APPWRITE_FUNCTION_ID.
set -euo pipefail
: "${APPWRITE_ENDPOINT:?}"; : "${APPWRITE_PROJECT_ID:?}"; : "${APPWRITE_API_KEY:?}"; : "${APPWRITE_FUNCTION_ID:?}"
REQ_PATH=${REQ_PATH:-/health}
REQ_METHOD=${REQ_METHOD:-GET}
REQ_BODY=${REQ_BODY:-}
EXTRA_HEADER=${EXTRA_HEADER:-}

headers_json="{\"content-type\":\"application/json\""
if [ -n "$EXTRA_HEADER" ]; then
  key=${EXTRA_HEADER%%:*}; value=${EXTRA_HEADER#*:}
  headers_json+=",\"${key}\":\"${value# }\""
fi
headers_json+="}"

payload=$(python3 - "$REQ_PATH" "$REQ_METHOD" "$REQ_BODY" "$headers_json" <<'PY'
import json, sys
path, method, body, headers = sys.argv[1:5]
print(json.dumps({"path": path, "method": method, "body": body, "headers": json.loads(headers)}))
PY
)

curl -sS -X POST "${APPWRITE_ENDPOINT%/}/functions/${APPWRITE_FUNCTION_ID}/executions" \
  -H "X-Appwrite-Project: ${APPWRITE_PROJECT_ID}" \
  -H "X-Appwrite-Key: ${APPWRITE_API_KEY}" \
  -H 'Content-Type: application/json' \
  -d "$payload"

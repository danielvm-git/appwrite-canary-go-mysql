#!/usr/bin/env bash
# migrate.sh — apply the embedded schema through the deployed function.
#
# The SQL lives in internal/schema/sql/ and is embedded in the binary, so the
# only way to change the database is POST /admin/migrate on a running function.
# CI calls exactly this right after a deploy (see .github/workflows/deploy.yml).
#
#   BASE=https://<site>/  is not used: the function is reached through Appwrite.
#   Locally: BASE=http://127.0.0.1:3000 MIGRATE_TOKEN=dev scripts/migrate.sh
set -euo pipefail

BASE=${BASE:-http://127.0.0.1:3000}
: "${MIGRATE_TOKEN:?MIGRATE_TOKEN is required}"

curl -fsS -X POST "${BASE%/}/admin/migrate" \
  -H "x-migrate-token: ${MIGRATE_TOKEN}" \
  -H 'Accept: application/json' | python3 -m json.tool

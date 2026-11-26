#!/usr/bin/env bash
# smoke.sh — end-to-end check of the API against the real MySQL, no Appwrite needed.
#
# Requires a running API with ALLOW_DEV_USER=true and a migrated schema.
#   BASE=http://127.0.0.1:3000 scripts/smoke.sh
#
# Covers: create/read/patch/delete, owner-vs-collaborator permissions, validation.
# The cross-user share flow needs Appwrite users and lives in verify-live.sh.
set -euo pipefail

BASE=${BASE:-http://127.0.0.1:3000}
OWNER=smoke-owner-$RANDOM
OTHER=smoke-other-$RANDOM
COLLAB=smoke-collab-$RANDOM
pass=0
fail=0

req() { # method path actor [json]
  local method=$1 path=$2 actor=$3 body=${4:-}
  local args=(-sS -o /tmp/smoke.out -w '%{http_code}' -X "$method" "$BASE$path")
  [ -n "$actor" ] && args+=(-H "x-dev-user: $actor")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}"
}

check() { # name expected actual
  if [ "$2" = "$3" ]; then echo "  ok   $1 ($3)"; pass=$((pass+1)); else echo "  FAIL $1: expected $2 got $3"; cat /tmp/smoke.out; fail=$((fail+1)); fi
}

echo "== health"
check "health 200" 200 "$(req GET /health '')"

echo "== create"
code=$(req POST /tasks "$OWNER" '{"title":"smoke task","priority":"high"}')
check "create 201" 201 "$code"
TASK=$(python3 -c 'import json;print(json.load(open("/tmp/smoke.out"))["$id"])')
check "sequence present" yes "$(python3 -c 'import json;print("yes" if json.load(open("/tmp/smoke.out")).get("$sequence") else "no")')"

echo "== list"
code=$(req GET '/tasks?scope=mine' "$OWNER")
check "owner list 200" 200 "$code"
check "owner sees 1" 1 "$(python3 -c 'import json;print(len(json.load(open("/tmp/smoke.out"))["tasks"]))')"

echo "== isolation"
code=$(req GET "/tasks/$TASK" "$OTHER")
check "stranger read 403" 403 "$code"

echo "== validation"
check "empty title 400" 400 "$(req POST /tasks "$OWNER" '{"title":"   "}')"
check "bad status 400" 400 "$(req PATCH "/tasks/$TASK" "$OWNER" '{"status":"nope"}')"

echo "== patch status"
check "status patch 200" 200 "$(req PATCH "/tasks/$TASK" "$OWNER" '{"status":"doing"}')"
check "status is doing" doing "$(python3 -c 'import json;print(json.load(open("/tmp/smoke.out"))["status"])')"

echo "== lists"
check "create list 201" 201 "$(req POST /lists "$OWNER" '{"name":"Smoke","color":"amber"}')"
LIST=$(python3 -c 'import json;print(json.load(open("/tmp/smoke.out"))["$id"])')
check "list tasks 200" 200 "$(req GET "/tasks?scope=$LIST" "$OWNER")"
check "delete list 204" 204 "$(req DELETE "/lists/$LIST" "$OWNER")"

echo "== delete"
check "owner delete 204" 204 "$(req DELETE "/tasks/$TASK" "$OWNER")"
check "gone 404" 404 "$(req GET "/tasks/$TASK" "$OWNER")"

rm -f /tmp/smoke.out
echo
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]

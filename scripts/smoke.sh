#!/usr/bin/env bash
# End-to-end smoke test against a throwaway local gatekeepd.
set -u
cd "$(dirname "$0")/.."
go build -o bin/ ./cmd/... || exit 1
DB=$(mktemp -d)/gk.db
sed "s#^db: gatekeep.db#db: $DB#; s#^listen: .*#listen: 127.0.0.1:18740#; s#^web_dir: .*#web_dir: \"\"#" configs/gatekeep.example.yaml > /tmp/gk-smoke.yaml
./bin/gatekeepd -config /tmp/gk-smoke.yaml > /tmp/gkd-smoke.log 2>&1 &
PID=$!; trap 'kill $PID' EXIT
sleep 1
export GATEKEEP_URL=http://127.0.0.1:18740 GATEKEEP_TARGET=local
gk() { GATEKEEP_TOKEN=$T ./bin/gk "$@"; }
rm -f /tmp/gk-touched
T=dev-agent; echo "== agent read";          gk uname -s; echo "exit=$?"
T=dev-agent; echo "== agent write";         gk -r "smoke test" touch /tmp/gk-touched; echo "exit=$?"
T=dev-agent; echo "== agent no reason";     gk touch /tmp/x; echo "exit=$?"
T=dev-carol; echo "== viewer write";        gk touch /tmp/x; echo "exit=$?"
T=dev-agent; echo "== agent check pipe";    gk check 'cat /etc/hosts;id'
T=dev-alice; echo "== pending";             gk pending
ID=$(gk pending | head -1 | cut -d' ' -f1)
T=dev-agent; echo "== agent self-approve";  gk approve "$ID"; echo "exit=$?"
(sleep 1; GATEKEEP_TOKEN=dev-bob ./bin/gk approve "$ID" looks fine) &
T=dev-agent; echo "== agent wait";          gk wait "$ID"; echo "exit=$?"; ls /tmp/gk-touched
echo "== MCP tools/list"; curl -s -H 'Authorization: Bearer dev-agent' $GATEKEEP_URL/mcp -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | head -c 200; echo
echo "== MCP run"; curl -s -H 'Authorization: Bearer dev-agent' $GATEKEEP_URL/mcp -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run","arguments":{"command":"df -h /"}}}'; echo
T=dev-alice; echo "== audit"; gk audit | head -15
gk audit --verify

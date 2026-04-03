#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

CONFIG_PATH="${1:-config.json}"
SAMPLE_ID="${2:-sample-$(date -u +%Y%m%dT%H%M%SZ)}"
QUESTION="${3:-广州天气}"
MODEL="${4:-deepseek-reasoner-search}"
API_KEY="${5:-}"
ADMIN_KEY="${DS2API_ADMIN_KEY:-admin}"

if [[ -z "$API_KEY" ]]; then
  API_KEY="$(python3 - <<'PY' "$CONFIG_PATH"
import json,sys
cfg=json.load(open(sys.argv[1]))
keys=cfg.get('keys') or []
print(keys[0] if keys else '')
PY
)"
fi

if [[ -z "$API_KEY" ]]; then
  echo "[capture] missing API key (pass as arg5 or set config.keys[0])" >&2
  exit 1
fi

OUT_DIR="tests/raw_stream_samples/${SAMPLE_ID}"
mkdir -p "$OUT_DIR"

cleanup() {
  pkill -f "cmd/ds2api" >/dev/null 2>&1 || true
}
trap cleanup EXIT

DS2API_CONFIG_PATH="$CONFIG_PATH" \
DS2API_ADMIN_KEY="$ADMIN_KEY" \
DS2API_DEV_PACKET_CAPTURE=1 \
DS2API_DEV_PACKET_CAPTURE_LIMIT=20 \
  go run ./cmd/ds2api >/tmp/ds2api_capture_server.log 2>&1 &

for _ in $(seq 1 120); do
  if curl -sSf http://127.0.0.1:5001/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

REQUEST_BODY="$(python3 - <<'PY' "$MODEL" "$QUESTION"
import json,sys
model,question=sys.argv[1:3]
payload={
  'model':model,
  'stream':True,
  'messages':[{'role':'user','content':question}],
}
print(json.dumps(payload, ensure_ascii=False))
PY
)"

curl -sS http://127.0.0.1:5001/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${API_KEY}" \
  --data-binary "${REQUEST_BODY}" \
  >"${OUT_DIR}/openai.stream.sse"

curl -sS http://127.0.0.1:5001/admin/dev/captures \
  -H "Authorization: Bearer ${ADMIN_KEY}" \
  >"${OUT_DIR}/captures.json"

python3 - <<'PY' "$OUT_DIR" "$SAMPLE_ID" "$QUESTION" "$MODEL"
import json,sys,pathlib,datetime
out=pathlib.Path(sys.argv[1])
sample_id,question,model=sys.argv[2:5]
captures=json.loads((out/'captures.json').read_text())
items=captures.get('items') or []
if not items:
  raise SystemExit('no captured upstream stream found')
best=max(items,key=lambda x:len((x.get('response_body') or '')))
raw=best.get('response_body') or ''
(out/'upstream.stream.sse').write_text(raw)
meta={
  'sample_id':sample_id,
  'captured_at_utc':datetime.datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%SZ'),
  'request':{'model':model,'stream':True,'messages':[{'role':'user','content':question}]},
  'capture':{
    'label':best.get('label'),'url':best.get('url'),'status_code':best.get('status_code'),
    'response_bytes':len(raw),'contains_finished_token':('FINISHED' in raw),'finished_token_count':raw.count('FINISHED')
  }
}
(out/'meta.json').write_text(json.dumps(meta,ensure_ascii=False,indent=2))
print(f'[capture] wrote sample to {out}')
print(f'[capture] upstream bytes={len(raw)} finished_count={raw.count("FINISHED")}')
PY

rm -f "${OUT_DIR}/captures.json"
echo "[capture] done: ${OUT_DIR}"

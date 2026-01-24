import base64
import binascii
import json
from typing import Any


def try_parse_json(raw: str) -> dict[str, Any] | None:
    try:
        val = json.loads(raw)
    except Exception:
        return None
    return val if isinstance(val, dict) else None


def safe_b64decode(raw: str) -> bytes | None:
    try:
        padding = "=" * (-len(raw) % 4)
        return base64.b64decode(raw + padding)
    except (binascii.Error, ValueError):
        return None


def try_decode_jwt_exp(token: str) -> int | None:
    parts = token.split(".")
    if len(parts) < 2:
        return None
    payload = safe_b64decode(parts[1])
    if not payload:
        return None
    try:
        data = json.loads(payload)
    except Exception:
        return None
    exp = data.get("exp")
    return int(exp) if isinstance(exp, (int, float)) else None


def compact_json_dumps(data: Any) -> str:
    return json.dumps(data, separators=(",", ":"), ensure_ascii=False)

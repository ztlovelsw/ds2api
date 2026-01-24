from __future__ import annotations

import json
from typing import Any


def estimate_tokens(text: Any) -> int:
    if text is None:
        return 0
    if isinstance(text, str):
        return len(text) // 4
    if isinstance(text, (bytes, bytearray)):
        return len(text) // 4
    if isinstance(text, list):
        return sum(estimate_tokens(item) for item in text)
    if isinstance(text, dict):
        if text.get("type") == "text":
            return estimate_tokens(text.get("text", ""))
        if text.get("type") == "tool_result":
            return estimate_tokens(text.get("content", ""))
        return estimate_tokens(json.dumps(text, ensure_ascii=False))
    return len(str(text)) // 4


def count_claude_tokens(payload: dict[str, Any]) -> int:
    messages = payload.get("messages", [])
    system = payload.get("system", "")
    tools = payload.get("tools", [])

    input_tokens = 0
    if system:
        input_tokens += estimate_tokens(system)

    for message in messages:
        role = message.get("role", "")
        content = message.get("content", "")

        input_tokens += 2
        input_tokens += estimate_tokens(role)

        if isinstance(content, list):
            for content_block in content:
                input_tokens += estimate_tokens(content_block)
        else:
            input_tokens += estimate_tokens(content)

    if tools:
        for tool in tools:
            input_tokens += estimate_tokens(tool.get("name", ""))
            input_tokens += estimate_tokens(tool.get("description", ""))
            input_tokens += estimate_tokens(json.dumps(tool.get("input_schema", {}), ensure_ascii=False))

    return max(1, input_tokens)

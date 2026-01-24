from __future__ import annotations

import re
from typing import Any


CLAUDE_DEFAULT_MODEL = "claude-sonnet-4-20250514"


def messages_prepare(messages: list[dict[str, Any]]) -> str:
    processed: list[dict[str, str]] = []
    for m in messages:
        role = str(m.get("role", ""))
        content = m.get("content", "")
        if isinstance(content, list):
            texts = [
                str(item.get("text", ""))
                for item in content
                if isinstance(item, dict) and item.get("type") == "text"
            ]
            text = "\n".join(texts)
        else:
            text = str(content)
        processed.append({"role": role, "text": text})

    if not processed:
        return ""

    merged = [processed[0]]
    for msg in processed[1:]:
        if msg["role"] == merged[-1]["role"]:
            merged[-1]["text"] += "\n\n" + msg["text"]
        else:
            merged.append(msg)

    parts: list[str] = []
    for idx, block in enumerate(merged):
        role = block["role"]
        text = block["text"]
        if role == "assistant":
            parts.append(f"<｜Assistant｜>{text}<｜end▁of▁sentence｜>")
        elif role in ("user", "system"):
            if idx > 0:
                parts.append(f"<｜User｜>{text}")
            else:
                parts.append(text)
        else:
            parts.append(text)

    final_prompt = "".join(parts)
    return re.sub(r"!\[(.*?)\]\((.*?)\)", r"[\1](\2)", final_prompt)


def normalize_claude_messages(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    normalized_messages: list[dict[str, Any]] = []
    for message in messages:
        normalized_message = dict(message)
        if isinstance(message.get("content"), list):
            content_parts: list[str] = []
            for content_block in message["content"]:
                if not isinstance(content_block, dict):
                    continue
                if content_block.get("type") == "text" and "text" in content_block:
                    content_parts.append(str(content_block["text"]))
                elif content_block.get("type") == "tool_result" and "content" in content_block:
                    content_parts.append(str(content_block["content"]))

            if content_parts:
                normalized_message["content"] = "\n".join(content_parts)
        normalized_messages.append(normalized_message)

    return normalized_messages


def convert_claude_to_deepseek(
    claude_request: dict[str, Any],
    *,
    default_model: str = CLAUDE_DEFAULT_MODEL,
    model_mapping: dict[str, str] | None = None,
) -> dict[str, Any]:
    messages = claude_request.get("messages", [])
    model = claude_request.get("model", default_model)

    mapping = model_mapping or {"fast": "deepseek-chat", "slow": "deepseek-chat"}

    model_lower = str(model).lower()
    if "opus" in model_lower or "reasoner" in model_lower or "slow" in model_lower:
        deepseek_model = mapping.get("slow", "deepseek-chat")
    else:
        deepseek_model = mapping.get("fast", "deepseek-chat")

    deepseek_request: dict[str, Any] = {"model": deepseek_model, "messages": list(messages)}

    if "system" in claude_request:
        system_msg = {"role": "system", "content": claude_request["system"]}
        deepseek_request["messages"].insert(0, system_msg)

    if "temperature" in claude_request:
        deepseek_request["temperature"] = claude_request["temperature"]
    if "top_p" in claude_request:
        deepseek_request["top_p"] = claude_request["top_p"]
    if "stop_sequences" in claude_request:
        deepseek_request["stop"] = claude_request["stop_sequences"]
    if "stream" in claude_request:
        deepseek_request["stream"] = claude_request["stream"]

    return deepseek_request

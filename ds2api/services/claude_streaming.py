from __future__ import annotations

import json
import random
import re
import time
from typing import Any, Iterator

from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


def detect_tool_calls(cleaned_response: str, tools_requested: list[dict[str, Any]]) -> list[dict[str, Any]]:
    detected_tools: list[dict[str, Any]] = []
    tool_detected = False

    if cleaned_response.startswith('{"tool_calls":') and cleaned_response.endswith(']}'):
        try:
            tool_data = json.loads(cleaned_response)
            for tool_call in tool_data.get("tool_calls", []):
                tool_name = tool_call.get("name")
                tool_input = tool_call.get("input", {})
                if any(tool.get("name") == tool_name for tool in tools_requested):
                    detected_tools.append({"name": tool_name, "input": tool_input})
                    tool_detected = True
        except json.JSONDecodeError:
            pass

    if not tool_detected:
        tool_call_pattern = r"\{\s*[\"\']tool_calls[\"\']\s*:\s*\[(.*?)\]\s*\}"
        matches = re.findall(tool_call_pattern, cleaned_response, re.DOTALL)
        for match in matches:
            try:
                tool_calls_json = f'{{"tool_calls": [{match}]}}'
                tool_data = json.loads(tool_calls_json)
                for tool_call in tool_data.get("tool_calls", []):
                    tool_name = tool_call.get("name")
                    tool_input = tool_call.get("input", {})
                    if any(tool.get("name") == tool_name for tool in tools_requested):
                        detected_tools.append({"name": tool_name, "input": tool_input})
                        tool_detected = True
            except json.JSONDecodeError:
                continue

    return detected_tools


def collect_deepseek_text(deepseek_resp) -> str:
    full_response_text = ""
    for line in deepseek_resp.iter_lines():
        if not line:
            continue
        try:
            line_str = line.decode("utf-8")
        except Exception:
            continue

        if not line_str.startswith("data:"):
            continue

        data_str = line_str[5:].strip()
        if data_str == "[DONE]":
            break

        try:
            chunk = json.loads(data_str)
        except Exception:
            continue

        if "v" in chunk and isinstance(chunk["v"], str):
            full_response_text += chunk["v"]
        elif "v" in chunk and isinstance(chunk["v"], list):
            for item in chunk["v"]:
                if item.get("p") == "status" and item.get("v") == "FINISHED":
                    break

    return full_response_text


def collect_deepseek_content_and_reasoning(deepseek_resp) -> tuple[str, str]:
    final_content = ""
    final_reasoning = ""
    ptype = "text"

    for raw_line in deepseek_resp.iter_lines():
        if not raw_line:
            continue
        try:
            line = raw_line.decode("utf-8")
        except Exception:
            continue

        if not line.startswith("data:"):
            continue

        data_str = line[5:].strip()
        if data_str == "[DONE]":
            break

        try:
            chunk = json.loads(data_str)
        except json.JSONDecodeError:
            continue

        if "v" not in chunk:
            continue

        v_value = chunk["v"]
        if chunk.get("p") == "response/thinking_content":
            ptype = "thinking"
        elif chunk.get("p") == "response/content":
            ptype = "text"

        if isinstance(v_value, str):
            if ptype == "thinking":
                final_reasoning += v_value
            else:
                final_content += v_value
        elif isinstance(v_value, list):
            for item in v_value:
                if item.get("p") == "status" and item.get("v") == "FINISHED":
                    break

    return final_content, final_reasoning


def claude_sse_stream(
    *,
    deepseek_resp,
    model: str,
    messages: list[dict[str, Any]],
    tools_requested: list[dict[str, Any]],
) -> Iterator[str]:
    message_id = f"msg_{int(time.time())}_{random.randint(1000, 9999)}"
    input_tokens = max(1, sum(len(str(m.get("content", ""))) for m in messages) // 4)

    try:
        full_response_text = collect_deepseek_text(deepseek_resp)
        cleaned_response = full_response_text.strip()
        detected_tools = detect_tool_calls(cleaned_response, tools_requested)

        message_start = {
            "type": "message_start",
            "message": {
                "id": message_id,
                "type": "message",
                "role": "assistant",
                "model": model,
                "content": [],
                "stop_reason": None,
                "stop_sequence": None,
                "usage": {"input_tokens": input_tokens, "output_tokens": 0},
            },
        }
        yield f"data: {json.dumps(message_start)}\n\n"

        content_index = 0
        if detected_tools:
            stop_reason = "tool_use"
            for tool_info in detected_tools:
                tool_use_id = f"toolu_{int(time.time())}_{random.randint(1000, 9999)}_{content_index}"
                yield (
                    "data: "
                    + json.dumps(
                        {
                            "type": "content_block_start",
                            "index": content_index,
                            "content_block": {
                                "type": "tool_use",
                                "id": tool_use_id,
                                "name": tool_info["name"],
                                "input": tool_info["input"],
                            },
                        }
                    )
                    + "\n\n"
                )
                yield (
                    "data: "
                    + json.dumps({"type": "content_block_stop", "index": content_index})
                    + "\n\n"
                )
                content_index += 1
        else:
            stop_reason = "end_turn"
            yield (
                "data: "
                + json.dumps(
                    {
                        "type": "content_block_start",
                        "index": 0,
                        "content_block": {"type": "text", "text": ""},
                    }
                )
                + "\n\n"
            )
            if cleaned_response:
                yield (
                    "data: "
                    + json.dumps(
                        {
                            "type": "content_block_delta",
                            "index": 0,
                            "delta": {"type": "text_delta", "text": cleaned_response},
                        }
                    )
                    + "\n\n"
                )
            yield "data: " + json.dumps({"type": "content_block_stop", "index": 0}) + "\n\n"

        output_tokens = max(1, len(cleaned_response) // 4)
        yield (
            "data: "
            + json.dumps(
                {
                    "type": "message_delta",
                    "delta": {"stop_reason": stop_reason, "stop_sequence": None},
                    "usage": {"output_tokens": output_tokens},
                }
            )
            + "\n\n"
        )
        yield "data: " + json.dumps({"type": "message_stop"}) + "\n\n"

    finally:
        try:
            deepseek_resp.close()
        except Exception:
            pass

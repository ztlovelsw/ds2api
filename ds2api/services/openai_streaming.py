from __future__ import annotations

import json
import queue
import threading
import time
from typing import Any, Iterator

from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


def openai_sse_stream(
    *,
    deepseek_resp,
    model: str,
    completion_id: str,
    created_time: int,
    final_prompt: str,
    thinking_enabled: bool,
    search_enabled: bool,
    keep_alive_timeout: int,
) -> Iterator[str]:
    final_text = ""
    final_thinking = ""
    first_chunk_sent = False
    result_queue: queue.Queue[dict[str, Any] | None] = queue.Queue()
    last_send_time = time.time()

    def process_data() -> None:
        ptype = "text"
        try:
            for raw_line in deepseek_resp.iter_lines():
                try:
                    line = raw_line.decode("utf-8")
                except Exception as e:
                    logger.warning(f"[sse_stream] 解码失败: {e}")
                    error_type = "thinking" if ptype == "thinking" else "text"
                    result_queue.put(
                        {
                            "choices": [
                                {
                                    "index": 0,
                                    "delta": {
                                        "content": "解码失败，请稍候再试",
                                        "type": error_type,
                                    },
                                }
                            ],
                            "model": "",
                            "chunk_token_usage": 1,
                            "created": 0,
                            "message_id": -1,
                            "parent_id": -1,
                        }
                    )
                    result_queue.put(None)
                    break

                if not line:
                    continue

                if not line.startswith("data:"):
                    continue

                data_str = line[5:].strip()
                if data_str == "[DONE]":
                    result_queue.put(None)
                    break

                try:
                    chunk = json.loads(data_str)
                    if "v" not in chunk:
                        continue

                    if chunk.get("p") == "response/search_status":
                        continue

                    if chunk.get("p") == "response/thinking_content":
                        ptype = "thinking"
                    elif chunk.get("p") == "response/content":
                        ptype = "text"

                    v_value = chunk["v"]
                    if isinstance(v_value, str):
                        content = v_value
                    elif isinstance(v_value, list):
                        for item in v_value:
                            if item.get("p") == "status" and item.get("v") == "FINISHED":
                                result_queue.put({"choices": [{"index": 0, "finish_reason": "stop"}]})
                                result_queue.put(None)
                                return
                        continue
                    else:
                        continue

                    result_queue.put(
                        {
                            "choices": [
                                {
                                    "index": 0,
                                    "delta": {"content": content, "type": ptype},
                                }
                            ],
                            "model": "",
                            "chunk_token_usage": len(content) // 4,
                            "created": 0,
                            "message_id": -1,
                            "parent_id": -1,
                        }
                    )
                except Exception as e:
                    logger.warning(f"[sse_stream] 无法解析: {data_str}, 错误: {e}")
                    error_type = "thinking" if ptype == "thinking" else "text"
                    result_queue.put(
                        {
                            "choices": [
                                {
                                    "index": 0,
                                    "delta": {
                                        "content": "解析失败，请稍候再试",
                                        "type": error_type,
                                    },
                                }
                            ],
                            "model": "",
                            "chunk_token_usage": 1,
                            "created": 0,
                            "message_id": -1,
                            "parent_id": -1,
                        }
                    )
                    result_queue.put(None)
                    break
        except Exception as e:
            logger.warning(f"[sse_stream] 错误: {e}")
            result_queue.put(
                {
                    "choices": [
                        {
                            "index": 0,
                            "delta": {
                                "content": "服务器错误，请稍候再试",
                                "type": "text",
                            },
                        }
                    ]
                }
            )
            result_queue.put(None)
        finally:
            try:
                deepseek_resp.close()
            except Exception:
                pass

    threading.Thread(target=process_data, daemon=True).start()

    while True:
        current_time = time.time()
        if current_time - last_send_time >= keep_alive_timeout:
            yield ": keep-alive\n\n"
            last_send_time = current_time
            continue

        try:
            chunk = result_queue.get(timeout=0.05)
        except queue.Empty:
            continue

        if chunk is None:
            prompt_tokens = len(final_prompt) // 4
            thinking_tokens = len(final_thinking) // 4
            completion_tokens = len(final_text) // 4
            usage = {
                "prompt_tokens": prompt_tokens,
                "completion_tokens": thinking_tokens + completion_tokens,
                "total_tokens": prompt_tokens + thinking_tokens + completion_tokens,
                "completion_tokens_details": {"reasoning_tokens": thinking_tokens},
            }
            finish_chunk = {
                "id": completion_id,
                "object": "chat.completion.chunk",
                "created": created_time,
                "model": model,
                "choices": [{"delta": {}, "index": 0, "finish_reason": "stop"}],
                "usage": usage,
            }
            yield f"data: {json.dumps(finish_chunk, ensure_ascii=False)}\n\n"
            yield "data: [DONE]\n\n"
            break

        new_choices = []
        for choice in chunk.get("choices", []):
            delta = choice.get("delta", {})
            ctype = delta.get("type")
            ctext = delta.get("content", "")
            if choice.get("finish_reason") == "backend_busy":
                ctext = "服务器繁忙，请稍候再试"
            if search_enabled and isinstance(ctext, str) and ctext.startswith("[citation:"):
                ctext = ""

            delta_obj: dict[str, Any] = {}
            if not first_chunk_sent:
                delta_obj["role"] = "assistant"
                first_chunk_sent = True

            if ctype == "thinking":
                if thinking_enabled:
                    final_thinking += ctext
                    delta_obj["reasoning_content"] = ctext
            elif ctype == "text":
                final_text += ctext
                delta_obj["content"] = ctext

            if delta_obj:
                new_choices.append({"delta": delta_obj, "index": choice.get("index", 0)})

        if new_choices:
            out_chunk = {
                "id": completion_id,
                "object": "chat.completion.chunk",
                "created": created_time,
                "model": model,
                "choices": new_choices,
            }
            yield f"data: {json.dumps(out_chunk, ensure_ascii=False)}\n\n"
            last_send_time = current_time


def openai_json_response_stream(
    *,
    deepseek_resp,
    model: str,
    completion_id: str,
    created_time: int,
    final_prompt: str,
    search_enabled: bool,
) -> Iterator[str]:
    think_list: list[str] = []
    text_list: list[str] = []
    result: dict[str, Any] | None = None

    def collect_data() -> None:
        nonlocal result
        ptype = "text"
        try:
            for raw_line in deepseek_resp.iter_lines():
                try:
                    line = raw_line.decode("utf-8")
                except Exception as e:
                    logger.warning(f"[chat_completions] 解码失败: {e}")
                    if ptype == "thinking":
                        think_list.append("解码失败，请稍候再试")
                    else:
                        text_list.append("解码失败，请稍候再试")
                    break

                if not line:
                    continue

                if not line.startswith("data:"):
                    continue

                data_str = line[5:].strip()
                if data_str == "[DONE]":
                    break

                try:
                    chunk = json.loads(data_str)
                    if "v" not in chunk:
                        continue

                    if chunk.get("p") == "response/search_status":
                        continue

                    if chunk.get("p") == "response/thinking_content":
                        ptype = "thinking"
                    elif chunk.get("p") == "response/content":
                        ptype = "text"

                    v_value = chunk["v"]
                    if isinstance(v_value, str):
                        if search_enabled and v_value.startswith("[citation:"):
                            continue
                        if ptype == "thinking":
                            think_list.append(v_value)
                        else:
                            text_list.append(v_value)
                    elif isinstance(v_value, list):
                        for item in v_value:
                            if item.get("p") == "status" and item.get("v") == "FINISHED":
                                final_reasoning = "".join(think_list)
                                final_content = "".join(text_list)
                                prompt_tokens = len(final_prompt) // 4
                                reasoning_tokens = len(final_reasoning) // 4
                                completion_tokens = len(final_content) // 4
                                result = {
                                    "id": completion_id,
                                    "object": "chat.completion",
                                    "created": created_time,
                                    "model": model,
                                    "choices": [
                                        {
                                            "index": 0,
                                            "message": {
                                                "role": "assistant",
                                                "content": final_content,
                                                "reasoning_content": final_reasoning,
                                            },
                                            "finish_reason": "stop",
                                        }
                                    ],
                                    "usage": {
                                        "prompt_tokens": prompt_tokens,
                                        "completion_tokens": reasoning_tokens + completion_tokens,
                                        "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                                        "completion_tokens_details": {
                                            "reasoning_tokens": reasoning_tokens
                                        },
                                    },
                                }
                                return
                except Exception as e:
                    logger.warning(f"[collect_data] 无法解析: {data_str}, 错误: {e}")
                    if ptype == "thinking":
                        think_list.append("解析失败，请稍候再试")
                    else:
                        text_list.append("解析失败，请稍候再试")
                    break
        except Exception as e:
            logger.warning(f"[collect_data] 错误: {e}")
            if ptype == "thinking":
                think_list.append("处理失败，请稍候再试")
            else:
                text_list.append("处理失败，请稍候再试")
        finally:
            try:
                deepseek_resp.close()
            except Exception:
                pass

            if result is None:
                final_content = "".join(text_list)
                final_reasoning = "".join(think_list)
                prompt_tokens = len(final_prompt) // 4
                reasoning_tokens = len(final_reasoning) // 4
                completion_tokens = len(final_content) // 4
                result = {
                    "id": completion_id,
                    "object": "chat.completion",
                    "created": created_time,
                    "model": model,
                    "choices": [
                        {
                            "index": 0,
                            "message": {
                                "role": "assistant",
                                "content": final_content,
                                "reasoning_content": final_reasoning,
                            },
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {
                        "prompt_tokens": prompt_tokens,
                        "completion_tokens": reasoning_tokens + completion_tokens,
                        "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                    },
                }

    t = threading.Thread(target=collect_data, daemon=True)
    t.start()

    while t.is_alive():
        time.sleep(0.1)

    yield json.dumps(result)

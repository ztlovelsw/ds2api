from __future__ import annotations

import random
import time
from typing import Any

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import JSONResponse, StreamingResponse
from starlette.background import BackgroundTask

from ds2api.core.auth import AuthContext, determine_claude_mode_and_token, get_auth_headers
from ds2api.core.message_processor import (
    convert_claude_to_deepseek,
    messages_prepare,
    normalize_claude_messages,
)
from ds2api.services.claude_streaming import (
    claude_sse_stream,
    collect_deepseek_content_and_reasoning,
    detect_tool_calls,
)
from ds2api.services.token_counter import count_claude_tokens
from ds2api.utils.logger import get_logger

logger = get_logger(__name__)

router = APIRouter()


async def _create_session_with_retry(ctx: AuthContext, *, max_attempts: int = 3) -> str | None:
    for _ in range(max_attempts):
        session_id = ctx.deepseek.create_session(ctx.token)
        if session_id:
            return session_id
        if ctx.use_config_token and await ctx.rotate_account():
            continue
    return None


async def _get_pow_with_retry(ctx: AuthContext, request: Request, *, max_attempts: int = 3) -> str | None:
    pow_service = request.app.state.pow

    for _ in range(max_attempts):
        challenge = ctx.deepseek.create_pow_challenge(ctx.token)
        if not challenge:
            if ctx.use_config_token and await ctx.rotate_account():
                continue
            continue

        pow_resp = await pow_service.solve_encoded_response(challenge)
        if pow_resp:
            return pow_resp

        if ctx.use_config_token and await ctx.rotate_account():
            continue

    return None


async def _call_deepseek_for_claude(ctx: AuthContext, request: Request, claude_payload: dict[str, Any]):
    cfg: dict[str, Any] = request.app.state.config

    deepseek_payload = convert_claude_to_deepseek(
        claude_payload,
        model_mapping=cfg.get("claude_model_mapping"),
    )

    model = deepseek_payload.get("model", "deepseek-chat")
    model_lower = str(model).lower()
    if model_lower in ["deepseek-v3", "deepseek-chat"]:
        thinking_enabled = False
        search_enabled = False
    elif model_lower in ["deepseek-r1", "deepseek-reasoner"]:
        thinking_enabled = True
        search_enabled = False
    elif model_lower in ["deepseek-v3-search", "deepseek-chat-search"]:
        thinking_enabled = False
        search_enabled = True
    elif model_lower in ["deepseek-r1-search", "deepseek-reasoner-search"]:
        thinking_enabled = True
        search_enabled = True
    else:
        thinking_enabled = False
        search_enabled = False

    final_prompt = messages_prepare(deepseek_payload.get("messages", []))

    session_id = await _create_session_with_retry(ctx)
    if not session_id:
        raise HTTPException(status_code=401, detail="invalid token.")

    pow_resp = await _get_pow_with_retry(ctx, request)
    if not pow_resp:
        raise HTTPException(
            status_code=401,
            detail="Failed to get PoW (invalid token or unknown error).",
        )

    headers = {**get_auth_headers(ctx.token), "x-ds-pow-response": pow_resp}
    payload = {
        "chat_session_id": session_id,
        "parent_message_id": None,
        "prompt": final_prompt,
        "ref_file_ids": [],
        "thinking_enabled": thinking_enabled,
        "search_enabled": search_enabled,
    }

    resp = ctx.deepseek.completion(headers=headers, payload=payload, max_attempts=3)
    return resp


@router.post("/anthropic/v1/messages")
async def claude_messages(request: Request):
    ctx: AuthContext | None = None
    try:
        try:
            ctx = await determine_claude_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        if not model or not messages:
            raise HTTPException(status_code=400, detail="Request must include 'model' and 'messages'.")

        normalized_messages = normalize_claude_messages(messages)
        tools_requested = req_data.get("tools") or []

        payload = dict(req_data)
        payload["messages"] = list(normalized_messages)

        if tools_requested and not any(m.get("role") == "system" for m in payload["messages"]):
            tool_schemas: list[str] = []
            for tool in tools_requested:
                tool_name = tool.get("name", "unknown")
                tool_desc = tool.get("description", "No description available")
                schema = tool.get("input_schema", {})

                tool_info = f"Tool: {tool_name}\nDescription: {tool_desc}"
                if isinstance(schema, dict) and "properties" in schema:
                    props = []
                    required = schema.get("required", [])
                    for prop_name, prop_info in schema["properties"].items():
                        prop_type = prop_info.get("type", "string") if isinstance(prop_info, dict) else "string"
                        is_req = " (required)" if prop_name in required else ""
                        props.append(f"  - {prop_name}: {prop_type}{is_req}")
                    if props:
                        tool_info += f"\nParameters:\n{chr(10).join(props)}"
                tool_schemas.append(tool_info)

            system_message = {
                "role": "system",
                "content": (
                    "You are Claude, a helpful AI assistant. You have access to these tools:\n\n"
                    + "\n".join(tool_schemas)
                    + "\n\nWhen you need to use tools, respond ONLY a JSON object with a tool_calls array."
                ),
            }
            payload["messages"].insert(0, system_message)

        deepseek_resp = await _call_deepseek_for_claude(ctx, request, payload)
        if not deepseek_resp:
            raise HTTPException(status_code=500, detail="Failed to get Claude response.")

        if deepseek_resp.status_code != 200:
            deepseek_resp.close()
            return JSONResponse(
                status_code=500,
                content={"error": {"type": "api_error", "message": "Failed to get response"}},
            )

        background = BackgroundTask(ctx.release) if ctx else None

        if bool(req_data.get("stream", False)):
            return StreamingResponse(
                claude_sse_stream(
                    deepseek_resp=deepseek_resp,
                    model=str(model),
                    messages=messages,
                    tools_requested=tools_requested,
                ),
                media_type="text/event-stream",
                headers={"Content-Type": "text/event-stream"},
                background=background,
            )

        try:
            final_content, final_reasoning = collect_deepseek_content_and_reasoning(deepseek_resp)
        finally:
            try:
                deepseek_resp.close()
            except Exception:
                pass

        cleaned_content = final_content.strip()
        detected_tools = detect_tool_calls(cleaned_content, tools_requested)

        claude_response: dict[str, Any] = {
            "id": f"msg_{int(time.time())}_{random.randint(1000, 9999)}",
            "type": "message",
            "role": "assistant",
            "model": model,
            "content": [],
            "stop_reason": "tool_use" if detected_tools else "end_turn",
            "stop_sequence": None,
            "usage": {
                "input_tokens": len(str(normalized_messages)) // 4,
                "output_tokens": (len(final_content) + len(final_reasoning)) // 4,
            },
        }

        if final_reasoning:
            claude_response["content"].append({"type": "thinking", "thinking": final_reasoning})

        if detected_tools:
            for i, tool_info in enumerate(detected_tools):
                tool_use_id = f"toolu_{int(time.time())}_{random.randint(1000, 9999)}_{i}"
                claude_response["content"].append(
                    {
                        "type": "tool_use",
                        "id": tool_use_id,
                        "name": tool_info["name"],
                        "input": tool_info["input"],
                    }
                )
        else:
            claude_response["content"].append(
                {"type": "text", "text": final_content or "抱歉，没有生成有效的响应内容。"}
            )

        return JSONResponse(content=claude_response, status_code=200, background=background)

    except HTTPException as exc:
        if ctx:
            await ctx.release()
        return JSONResponse(
            status_code=exc.status_code,
            content={"error": {"type": "invalid_request_error", "message": exc.detail}},
        )
    except Exception as exc:
        logger.error(f"[claude_messages] 未知异常: {exc}")
        if ctx:
            await ctx.release()
        return JSONResponse(
            status_code=500,
            content={"error": {"type": "api_error", "message": "Internal Server Error"}},
        )


@router.post("/anthropic/v1/messages/count_tokens")
async def claude_count_tokens(request: Request):
    ctx: AuthContext | None = None
    try:
        try:
            ctx = await determine_claude_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})

        req_data = await request.json()
        if not req_data.get("model") or not req_data.get("messages"):
            raise HTTPException(status_code=400, detail="Request must include 'model' and 'messages'.")

        response = {"input_tokens": count_claude_tokens(req_data)}
        background = BackgroundTask(ctx.release) if ctx else None
        return JSONResponse(content=response, status_code=200, background=background)

    except HTTPException as exc:
        if ctx:
            await ctx.release()
        return JSONResponse(
            status_code=exc.status_code,
            content={"error": {"type": "invalid_request_error", "message": exc.detail}},
        )
    except Exception as exc:
        logger.error(f"[claude_count_tokens] 未知异常: {exc}")
        if ctx:
            await ctx.release()
        return JSONResponse(
            status_code=500,
            content={"error": {"type": "api_error", "message": "Internal Server Error"}},
        )

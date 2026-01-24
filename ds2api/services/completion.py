from __future__ import annotations

import time
from typing import Any

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import JSONResponse, StreamingResponse
from starlette.background import BackgroundTask

from ds2api.core.auth import AuthContext, determine_mode_and_token, get_auth_headers
from ds2api.core.message_processor import messages_prepare
from ds2api.services.openai_streaming import openai_json_response_stream, openai_sse_stream
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


@router.post("/v1/chat/completions")
async def chat_completions(request: Request):
    ctx: AuthContext | None = None
    try:
        try:
            ctx = await determine_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        if not model or not messages:
            raise HTTPException(status_code=400, detail="Request must include 'model' and 'messages'.")

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
            raise HTTPException(status_code=503, detail=f"Model '{model}' is not available.")

        final_prompt = messages_prepare(messages)

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
        payload: dict[str, Any] = {
            "chat_session_id": session_id,
            "parent_message_id": None,
            "prompt": final_prompt,
            "ref_file_ids": [],
            "thinking_enabled": thinking_enabled,
            "search_enabled": search_enabled,
        }

        deepseek_resp = ctx.deepseek.completion(headers=headers, payload=payload, max_attempts=3)
        if not deepseek_resp:
            raise HTTPException(status_code=500, detail="Failed to get completion.")

        created_time = int(time.time())
        completion_id = f"{session_id}"
        keep_alive_timeout = request.app.state.settings.keep_alive_timeout

        background = BackgroundTask(ctx.release) if ctx else None

        if bool(req_data.get("stream", False)):
            if deepseek_resp.status_code != 200:
                deepseek_resp.close()
                return JSONResponse(content=deepseek_resp.content, status_code=deepseek_resp.status_code)

            return StreamingResponse(
                openai_sse_stream(
                    deepseek_resp=deepseek_resp,
                    model=str(model),
                    completion_id=completion_id,
                    created_time=created_time,
                    final_prompt=final_prompt,
                    thinking_enabled=thinking_enabled,
                    search_enabled=search_enabled,
                    keep_alive_timeout=keep_alive_timeout,
                ),
                media_type="text/event-stream",
                headers={"Content-Type": "text/event-stream"},
                background=background,
            )

        return StreamingResponse(
            openai_json_response_stream(
                deepseek_resp=deepseek_resp,
                model=str(model),
                completion_id=completion_id,
                created_time=created_time,
                final_prompt=final_prompt,
                search_enabled=search_enabled,
            ),
            media_type="application/json",
            background=background,
        )

    except HTTPException as exc:
        if ctx:
            await ctx.release()
        return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})
    except Exception as exc:
        logger.error(f"[chat_completions] 未知异常: {exc}")
        if ctx:
            await ctx.release()
        return JSONResponse(status_code=500, content={"error": "Internal Server Error"})

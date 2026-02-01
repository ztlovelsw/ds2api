# -*- coding: utf-8 -*-
"""OpenAI 兼容路由"""
import json
import queue
import re
import threading
import time

from curl_cffi import requests as cffi_requests
from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import JSONResponse, StreamingResponse

from core.config import CONFIG, logger
from core.auth import (
    determine_mode_and_token,
    get_auth_headers,
    release_account,
)
from core.deepseek import call_completion_endpoint
from core.session_manager import (
    create_session,
    get_pow,
    cleanup_account,
)
from core.models import get_model_config, get_openai_models_response
from core.stream_parser import (
    parse_deepseek_sse_line,
    extract_content_from_chunk,
    should_filter_citation,
)
from core.messages import messages_prepare

router = APIRouter()

# 添加保活超时配置（5秒）
KEEP_ALIVE_TIMEOUT = 5

# 预编译正则表达式（性能优化）
_CITATION_PATTERN = re.compile(r"^\[citation:")


# ----------------------------------------------------------------------
# 路由：/v1/models
# ----------------------------------------------------------------------
@router.get("/v1/models")
def list_models():
    data = get_openai_models_response()
    return JSONResponse(content=data, status_code=200)


# ----------------------------------------------------------------------
# 路由：/v1/chat/completions
# ----------------------------------------------------------------------
@router.post("/v1/chat/completions")
async def chat_completions(request: Request):
    try:
        # 处理 token 相关逻辑，若登录失败则直接返回错误响应
        try:
            determine_mode_and_token(request)
        except HTTPException as exc:
            return JSONResponse(
                status_code=exc.status_code, content={"error": exc.detail}
            )
        except Exception as exc:
            logger.error(f"[chat_completions] determine_mode_and_token 异常: {exc}")
            return JSONResponse(
                status_code=500, content={"error": "Account login failed."}
            )

        req_data = await request.json()
        model = req_data.get("model")
        messages = req_data.get("messages", [])
        if not model or not messages:
            raise HTTPException(
                status_code=400, detail="Request must include 'model' and 'messages'."
            )
        
        # 使用会话管理器获取模型配置
        thinking_enabled, search_enabled = get_model_config(model)
        if thinking_enabled is None:
            raise HTTPException(
                status_code=503, detail=f"Model '{model}' is not available."
            )
        
        # 使用 messages_prepare 函数构造最终 prompt
        final_prompt = messages_prepare(messages)
        session_id = create_session(request)
        if not session_id:
            raise HTTPException(status_code=401, detail="invalid token.")
        
        pow_resp = get_pow(request)
        if not pow_resp:
            raise HTTPException(
                status_code=401,
                detail="Failed to get PoW (invalid token or unknown error).",
            )
        
        headers = {**get_auth_headers(request), "x-ds-pow-response": pow_resp}
        payload = {
            "chat_session_id": session_id,
            "parent_message_id": None,
            "prompt": final_prompt,
            "ref_file_ids": [],
            "thinking_enabled": thinking_enabled,
            "search_enabled": search_enabled,
        }

        deepseek_resp = call_completion_endpoint(payload, headers, max_attempts=3)
        if not deepseek_resp:
            raise HTTPException(status_code=500, detail="Failed to get completion.")
        created_time = int(time.time())
        completion_id = f"{session_id}"

        # 流式响应（SSE）或普通响应
        if bool(req_data.get("stream", False)):
            if deepseek_resp.status_code != 200:
                deepseek_resp.close()
                return JSONResponse(
                    content=deepseek_resp.content, status_code=deepseek_resp.status_code
                )

            def sse_stream():
                # 智能超时配置
                STREAM_IDLE_TIMEOUT = 30  # 无新内容超时（秒）
                MAX_KEEPALIVE_COUNT = 10  # 最大连续 keepalive 次数
                
                try:
                    final_text = ""
                    final_thinking = ""
                    first_chunk_sent = False
                    result_queue = queue.Queue()
                    last_send_time = time.time()
                    last_content_time = time.time()  # 最后收到有效内容的时间
                    keepalive_count = 0  # 连续 keepalive 计数
                    has_content = False  # 是否收到过内容

                    def process_data():
                        nonlocal has_content
                        ptype = "text"
                        current_fragment_type = "thinking" if thinking_enabled else "text"  # 追踪当前活跃的 fragment 类型
                        logger.info(f"[sse_stream] 开始处理数据流, session_id={session_id}")
                        try:
                            for raw_line in deepseek_resp.iter_lines():
                                try:
                                    line = raw_line.decode("utf-8")
                                except Exception as e:
                                    logger.warning(f"[sse_stream] 解码失败: {e}")
                                    error_type = "thinking" if ptype == "thinking" else "text"
                                    busy_content_str = f'{{"choices":[{{"index":0,"delta":{{"content":"解码失败，请稍候再试","type":"{error_type}"}}}}],"model":"","chunk_token_usage":1,"created":0,"message_id":-1,"parent_id":-1}}'
                                    try:
                                        busy_content = json.loads(busy_content_str)
                                        result_queue.put(busy_content)
                                    except json.JSONDecodeError:
                                        result_queue.put({"choices": [{"index": 0, "delta": {"content": "解码失败", "type": "text"}}]})
                                    result_queue.put(None)
                                    break
                                if not line:
                                    continue
                                if line.startswith("data:"):
                                    data_str = line[5:].strip()
                                    if data_str == "[DONE]":
                                        result_queue.put(None)
                                        break
                                    try:
                                        chunk = json.loads(data_str)
                                        
                                        # 检测内容审核/敏感词阻止
                                        if "error" in chunk or chunk.get("code") == "content_filter":
                                            logger.warning(f"[sse_stream] 检测到内容过滤: {chunk}")
                                            result_queue.put({"choices": [{"index": 0, "finish_reason": "content_filter"}]})
                                            result_queue.put(None)
                                            return
                                        
                                        logger.info(f"[sse_stream] RAW 原始chunk: {data_str[:300]}")
                                        print(f"[DEBUG] RAW: {data_str[:300]}", flush=True)
                                        
                                        # 写入原始 chunk 到日志文件
                                        with open("/tmp/ds2api_debug.log", "a") as f:
                                            f.write(f"[MAIN] chunk_path={chunk.get('p', '')}, v_type={type(chunk.get('v')).__name__}, chunk={str(chunk)[:300]}\n")
                                        
                                        if "v" in chunk:
                                            v_value = chunk["v"]
                                            content = ""
                                            chunk_path = chunk.get("p", "")
                                            
                                            if chunk_path == "response/search_status":
                                                continue
                                            
                                            # 跳过所有状态相关的 chunk（不是内容）
                                            # 注意：response/status 是真正的结束信号，需要特殊处理（后面的代码会处理）
                                            # 但 response/fragments/-1/status 等需要跳过
                                            skip_patterns = [
                                                "quasi_status", "elapsed_secs", "token_usage", 
                                                "pending_fragment", "conversation_mode",
                                                "fragments/-1/status", "fragments/-2/status"  # 搜索片段状态
                                            ]
                                            if any(kw in chunk_path for kw in skip_patterns):
                                                continue
                                            
                                            # 检查是否是真正的响应结束信号
                                            if chunk_path == "response/status" and isinstance(v_value, str) and v_value == "FINISHED":
                                                result_queue.put({"choices": [{"index": 0, "finish_reason": "stop"}]})
                                                result_queue.put(None)
                                                return
                                            
                                            # 检测 fragment 类型变化（来自直接的 fragments 路径或 BATCH 操作）
                                            new_fragment_type = current_fragment_type
                                            
                                            # 检测 BATCH APPEND 格式: {'p': 'response', 'o': 'BATCH', 'v': [...]}
                                            if chunk_path == "response" and isinstance(v_value, list):
                                                for batch_item in v_value:
                                                    if isinstance(batch_item, dict) and batch_item.get("p") == "fragments" and batch_item.get("o") == "APPEND":
                                                        fragments = batch_item.get("v", [])
                                                        for frag in fragments:
                                                            if isinstance(frag, dict):
                                                                frag_type = frag.get("type", "").upper()
                                                                if frag_type == "THINK" or frag_type == "THINKING":
                                                                    new_fragment_type = "thinking"
                                                                elif frag_type == "RESPONSE":
                                                                    new_fragment_type = "text"
                                            
                                            # 也检测直接的 fragments 路径
                                            if "response/fragments" in chunk_path and isinstance(v_value, list):
                                                for frag in v_value:
                                                    if isinstance(frag, dict):
                                                        frag_type = frag.get("type", "").upper()
                                                        if frag_type == "THINK" or frag_type == "THINKING":
                                                            new_fragment_type = "thinking"
                                                        elif frag_type == "RESPONSE":
                                                            new_fragment_type = "text"
                                            
                                            # 确定当前内容类型
                                            if chunk_path == "response/thinking_content":
                                                ptype = "thinking"
                                            elif chunk_path == "response/content":
                                                ptype = "text"
                                            elif "response/fragments" in chunk_path and "/content" in chunk_path:
                                                # 如 response/fragments/-1/content - 使用当前 fragment 类型
                                                ptype = current_fragment_type
                                            elif "response/fragments" in chunk_path:
                                                # fragments 的类型由内层 type 决定，默认用之前的 ptype
                                                pass
                                            elif not chunk_path:
                                                # 空路径内容：使用当前活跃的 fragment 类型
                                                if thinking_enabled:
                                                    ptype = current_fragment_type
                                                else:
                                                    ptype = "text"
                                            
                                            # 更新 current_fragment_type 供后续处理使用
                                            current_fragment_type = new_fragment_type
                                            
                                            logger.info(f"[sse_stream] ptype={ptype}, current_fragment_type={current_fragment_type}, chunk_path='{chunk_path}', v_type={type(v_value).__name__}, v={str(v_value)[:100]}")
                                            if isinstance(v_value, str):
                                                # 检查是否是 FINISHED 状态
                                                # 只有当 chunk_path 为空或为 "status" 时才认为是真正的结束
                                                # 搜索模型会发送 "response/fragments/-1/status": "FINISHED" 表示搜索片段完成，不是响应结束
                                                if v_value == "FINISHED" and (not chunk_path or chunk_path == "status"):
                                                    result_queue.put({"choices": [{"index": 0, "finish_reason": "stop"}]})
                                                    result_queue.put(None)
                                                    return
                                                content = v_value
                                                if content:
                                                    has_content = True
                                            elif isinstance(v_value, list):
                                                # DeepSeek 可能发送嵌套列表格式
                                                # 需要递归提取内容
                                                def extract_content_recursive(items, default_type="text"):
                                                    """递归提取列表中的内容"""
                                                    extracted = []
                                                    for item in items:
                                                        if not isinstance(item, dict):
                                                            continue
                                                        
                                                        item_p = item.get("p", "")
                                                        item_v = item.get("v")
                                                        
                                                        # 写入调试日志 - 显示完整的 item
                                                        with open("/tmp/ds2api_debug.log", "a") as f:
                                                            f.write(f"[extract] full_item={str(item)[:200]}\n")
                                                        
                                                        # 跳过搜索结果项（包含 url/title/snippet 的项目）
                                                        if "url" in item and "title" in item:
                                                            continue
                                                        
                                                        # 跳过 quasi_status（搜索完成信号，不是响应完成）
                                                        if item_p == "quasi_status":
                                                            continue
                                                        
                                                        # 跳过 accumulated_token_usage 和 has_pending_fragment
                                                        if item_p in ("accumulated_token_usage", "has_pending_fragment"):
                                                            continue
                                                        
                                                        # 只有当 p="status" (精确匹配) 且 v="FINISHED" 才认为是真正结束
                                                        if item_p == "status" and item_v == "FINISHED":
                                                            return None  # 信号结束
                                                        
                                                        # 跳过搜索状态
                                                        if item_p == "response/search_status":
                                                            continue
                                                        
                                                        # 直接处理包含 content 和 type 的项 (例如 {'id': 2, 'type': 'RESPONSE', 'content': '...'})
                                                        if "content" in item and "type" in item:
                                                            inner_type = item.get("type", "").upper()
                                                            if inner_type == "THINK" or inner_type == "THINKING":
                                                                final_type = "thinking"
                                                            elif inner_type == "RESPONSE":
                                                                final_type = "text"
                                                            else:
                                                                final_type = default_type
                                                            content = item.get("content", "")
                                                            if content:
                                                                extracted.append((content, final_type))
                                                            continue
                                                        
                                                        # 确定类型（基于 p 字段）
                                                        if "thinking" in item_p:
                                                            content_type = "thinking"
                                                        elif "content" in item_p or item_p == "response" or item_p == "fragments":
                                                            content_type = "text"
                                                        else:
                                                            content_type = default_type
                                                        
                                                        # 处理不同的 v 类型
                                                        if isinstance(item_v, str):
                                                            if item_v and item_v != "FINISHED":
                                                                extracted.append((item_v, content_type))
                                                        elif isinstance(item_v, list):
                                                            # 内层可能是 [{"content": "text", "type": "THINK/RESPONSE", ...}] 格式
                                                            for inner in item_v:
                                                                if isinstance(inner, dict):
                                                                    # 检查内层的 type 字段
                                                                    inner_type = inner.get("type", "").upper()
                                                                    # DeepSeek 使用 THINK 而不是 THINKING
                                                                    if inner_type == "THINK" or inner_type == "THINKING":
                                                                        final_type = "thinking"
                                                                    elif inner_type == "RESPONSE":
                                                                        final_type = "text"
                                                                    else:
                                                                        final_type = content_type  # 继承外层类型
                                                                    
                                                                    content = inner.get("content", "")
                                                                    if content:
                                                                        extracted.append((content, final_type))
                                                                elif isinstance(inner, str) and inner:
                                                                    extracted.append((inner, content_type))
                                                    return extracted
                                                
                                                result = extract_content_recursive(v_value, ptype)
                                                
                                                if result is None:
                                                    # FINISHED 信号
                                                    result_queue.put({"choices": [{"index": 0, "finish_reason": "stop"}]})
                                                    result_queue.put(None)
                                                    return
                                                
                                                for content_text, content_type in result:
                                                    if content_text:
                                                        logger.debug(f"[sse_stream] 提取内容: {content_text[:30] if len(content_text) > 30 else content_text}")
                                                        chunk = {
                                                            "choices": [{
                                                                "index": 0,
                                                                "delta": {"content": content_text, "type": content_type}
                                                            }],
                                                            "model": "",
                                                            "chunk_token_usage": len(content_text) // 4,
                                                            "created": 0,
                                                            "message_id": -1,
                                                            "parent_id": -1
                                                        }
                                                        result_queue.put(chunk)
                                                        has_content = True

                                                continue
                                            
                                            unified_chunk = {
                                                "choices": [{
                                                    "index": 0,
                                                    "delta": {"content": content, "type": ptype}
                                                }],
                                                "model": "",
                                                "chunk_token_usage": len(content) // 4,
                                                "created": 0,
                                                "message_id": -1,
                                                "parent_id": -1
                                            }
                                            result_queue.put(unified_chunk)
                                            

                                    except Exception as e:
                                        logger.warning(f"[sse_stream] 无法解析: {data_str}, 错误: {e}")
                                        error_type = "thinking" if ptype == "thinking" else "text"
                                        busy_content_str = f'{{"choices":[{{"index":0,"delta":{{"content":"解析失败，请稍候再试","type":"{error_type}"}}}}],"model":"","chunk_token_usage":1,"created":0,"message_id":-1,"parent_id":-1}}'
                                        try:
                                            busy_content = json.loads(busy_content_str)
                                            result_queue.put(busy_content)
                                        except json.JSONDecodeError:
                                            result_queue.put({"choices": [{"index": 0, "delta": {"content": "解析失败", "type": "text"}}]})
                                        result_queue.put(None)
                                        break
                        except Exception as e:
                            logger.warning(f"[sse_stream] 错误: {e}")
                            try:
                                error_response = {"choices": [{"index": 0, "delta": {"content": "服务器错误，请稍候再试", "type": "text"}}]}
                                result_queue.put(error_response)
                            except Exception:
                                pass
                            result_queue.put(None)
                        finally:
                            deepseek_resp.close()

                    process_thread = threading.Thread(target=process_data)
                    process_thread.start()

                    while True:
                        current_time = time.time()
                        
                        # 智能超时检测：如果已有内容且长时间无新数据，强制结束
                        if has_content and (current_time - last_content_time) > STREAM_IDLE_TIMEOUT:
                            logger.warning(f"[sse_stream] 智能超时: 已有内容但 {STREAM_IDLE_TIMEOUT}s 无新数据，强制结束")
                            break
                        
                        # 连续 keepalive 检测：如果已有内容且连续多次 keepalive，强制结束
                        if has_content and keepalive_count >= MAX_KEEPALIVE_COUNT:
                            logger.warning(f"[sse_stream] 智能超时: 连续 {MAX_KEEPALIVE_COUNT} 次 keepalive，强制结束")
                            break
                        
                        if current_time - last_send_time >= KEEP_ALIVE_TIMEOUT:
                            yield ": keep-alive\n\n"
                            last_send_time = current_time
                            keepalive_count += 1
                            continue
                            
                        try:
                            chunk = result_queue.get(timeout=0.05)
                            keepalive_count = 0  # 重置 keepalive 计数
                            
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
                                last_send_time = current_time
                                break
                                
                            new_choices = []
                            for choice in chunk.get("choices", []):
                                delta = choice.get("delta", {})
                                ctype = delta.get("type")
                                ctext = delta.get("content", "")
                                if choice.get("finish_reason") == "backend_busy":
                                    ctext = "服务器繁忙，请稍候再试"
                                if choice.get("finish_reason") == "content_filter":
                                    # 内容过滤，正常结束
                                    pass
                                if search_enabled and ctext.startswith("[citation:"):
                                    ctext = ""
                                if ctype == "thinking":
                                    if thinking_enabled:
                                        final_thinking += ctext
                                else:
                                    # 非 thinking 内容都作为普通文本处理（包括 ctype=None 或 "text"）
                                    final_text += ctext
                                delta_obj = {}
                                if not first_chunk_sent:
                                    delta_obj["role"] = "assistant"
                                    first_chunk_sent = True
                                if ctype == "thinking":
                                    if thinking_enabled:
                                        delta_obj["reasoning_content"] = ctext
                                else:
                                    # 非 thinking 内容都作为 content 输出
                                    if ctext:
                                        delta_obj["content"] = ctext
                                if delta_obj:
                                    new_choices.append({"delta": delta_obj, "index": choice.get("index", 0)})
                                    
                            if new_choices:
                                last_content_time = current_time  # 更新最后内容时间
                                out_chunk = {
                                    "id": completion_id,
                                    "object": "chat.completion.chunk",
                                    "created": created_time,
                                    "model": model,
                                    "choices": new_choices,
                                }
                                yield f"data: {json.dumps(out_chunk, ensure_ascii=False)}\n\n"
                                last_send_time = current_time
                        except queue.Empty:
                            continue
                            
                    # 如果是超时退出，也发送结束标记
                    if has_content:
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
                        
                except Exception as e:
                    logger.error(f"[sse_stream] 异常: {e}")
                finally:
                    cleanup_account(request)

            return StreamingResponse(
                sse_stream(),
                media_type="text/event-stream",
                headers={"Content-Type": "text/event-stream"},
            )
        else:
            # 非流式响应处理
            think_list = []
            text_list = []
            result = None

            data_queue = queue.Queue()

            def collect_data():
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
                            data_queue.put(None)
                            break
                        if not line:
                            continue
                        if line.startswith("data:"):
                            data_str = line[5:].strip()
                            if data_str == "[DONE]":
                                data_queue.put(None)
                                break
                            try:
                                chunk = json.loads(data_str)
                                if "v" in chunk:
                                    v_value = chunk["v"]
                                    if "p" in chunk and chunk.get("p") == "response/search_status":
                                        continue
                                    if "p" in chunk and chunk.get("p") == "response/thinking_content":
                                        ptype = "thinking"
                                    elif "p" in chunk and chunk.get("p") == "response/content":
                                        ptype = "text"
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
                                                # 构建 message 对象
                                                message_obj = {
                                                    "role": "assistant",
                                                    "content": final_content,
                                                }
                                                # 只有启用思考模式时才包含 reasoning_content
                                                if thinking_enabled and final_reasoning:
                                                    message_obj["reasoning_content"] = final_reasoning
                                                
                                                result = {
                                                    "id": completion_id,
                                                    "object": "chat.completion",
                                                    "created": created_time,
                                                    "model": model,
                                                    "choices": [{
                                                        "index": 0,
                                                        "message": message_obj,
                                                        "finish_reason": "stop",
                                                    }],
                                                    "usage": {
                                                        "prompt_tokens": prompt_tokens,
                                                        "completion_tokens": reasoning_tokens + completion_tokens,
                                                        "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                                                        "completion_tokens_details": {"reasoning_tokens": reasoning_tokens},
                                                    },
                                                }
                                                data_queue.put("DONE")
                                                return
                            except Exception as e:
                                logger.warning(f"[collect_data] 无法解析: {data_str}, 错误: {e}")
                                if ptype == "thinking":
                                    think_list.append("解析失败，请稍候再试")
                                else:
                                    text_list.append("解析失败，请稍候再试")
                                data_queue.put(None)
                                break
                except Exception as e:
                    logger.warning(f"[collect_data] 错误: {e}")
                    if ptype == "thinking":
                        think_list.append("处理失败，请稍候再试")
                    else:
                        text_list.append("处理失败，请稍候再试")
                    data_queue.put(None)
                finally:
                    deepseek_resp.close()
                    if result is None:
                        final_content = "".join(text_list)
                        final_reasoning = "".join(think_list)
                        prompt_tokens = len(final_prompt) // 4
                        reasoning_tokens = len(final_reasoning) // 4
                        completion_tokens = len(final_content) // 4
                        # 构建 message 对象
                        message_obj = {
                            "role": "assistant",
                            "content": final_content,
                        }
                        # 只有启用思考模式时才包含 reasoning_content
                        if thinking_enabled and final_reasoning:
                            message_obj["reasoning_content"] = final_reasoning
                        
                        result = {
                            "id": completion_id,
                            "object": "chat.completion",
                            "created": created_time,
                            "model": model,
                            "choices": [{
                                "index": 0,
                                "message": message_obj,
                                "finish_reason": "stop",
                            }],
                            "usage": {
                                "prompt_tokens": prompt_tokens,
                                "completion_tokens": reasoning_tokens + completion_tokens,
                                "total_tokens": prompt_tokens + reasoning_tokens + completion_tokens,
                            },
                        }
                    data_queue.put("DONE")

            collect_thread = threading.Thread(target=collect_data)
            collect_thread.start()

            def generate():
                last_send_time = time.time()
                while True:
                    current_time = time.time()
                    if current_time - last_send_time >= KEEP_ALIVE_TIMEOUT:
                        yield ""
                        last_send_time = current_time
                    if not collect_thread.is_alive() and result is not None:
                        yield json.dumps(result)
                        break
                    time.sleep(0.1)

            return StreamingResponse(generate(), media_type="application/json")
    except HTTPException as exc:
        return JSONResponse(status_code=exc.status_code, content={"error": exc.detail})
    except Exception as exc:
        logger.error(f"[chat_completions] 未知异常: {exc}")
        return JSONResponse(status_code=500, content={"error": "Internal Server Error"})
    finally:
        cleanup_account(request)

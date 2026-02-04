# -*- coding: utf-8 -*-
"""Admin 账号管理模块 - 账号验证和测试"""
import asyncio
import json
import base64

from fastapi import APIRouter, HTTPException, Request, Depends
from fastapi.responses import JSONResponse

from core.config import CONFIG, save_config, logger, WASM_PATH
from core.auth import init_account_queue, get_account_identifier
from core.deepseek import (
    login_deepseek_via_account, 
    DEEPSEEK_CREATE_SESSION_URL, 
    DEEPSEEK_COMPLETION_URL, 
    BASE_HEADERS,
)
from core.pow import compute_pow_answer
from core.models import get_model_config
from core.sse_parser import parse_sse_chunk_for_content

from .auth import verify_admin

router = APIRouter()


# ----------------------------------------------------------------------
# 账号验证
# ----------------------------------------------------------------------
async def validate_single_account(account: dict) -> dict:
    """验证单个账号的有效性"""
    acc_id = get_account_identifier(account)
    result = {
        "account": acc_id,
        "valid": False,
        "has_token": bool(account.get("token", "").strip()),
        "message": "",
    }

    def _is_token_invalid(status_code: int, data: dict) -> bool:
        msg = (data.get("msg") or data.get("message") or "").lower()
        code = data.get("code")
        return status_code in {401, 403} or code in {40001, 40002, 40003} or "token" in msg or "unauthorized" in msg

    def _create_session(token: str) -> dict:
        headers = {**BASE_HEADERS, "authorization": f"Bearer {token}"}
        try:
            session_resp = cffi_requests.post(
                DEEPSEEK_CREATE_SESSION_URL,
                headers=headers,
                json={"agent": "chat"},
                impersonate="safari15_3",
                timeout=15,
            )
        except Exception as e:
            return {"success": False, "message": f"请求异常: {e}", "status_code": 0, "data": {}}

        try:
            data = session_resp.json()
        except Exception:
            data = {}
        finally:
            session_resp.close()
        if session_resp.status_code == 200 and data.get("code") == 0:
            return {
                "success": True,
                "session_id": data.get("data", {}).get("biz_data", {}).get("id"),
                "status_code": session_resp.status_code,
                "data": data,
            }
        return {
            "success": False,
            "message": data.get("msg") or f"HTTP {session_resp.status_code}",
            "status_code": session_resp.status_code,
            "data": data,
        }

    try:
        token = account.get("token", "").strip()
        if token:
            session_result = _create_session(token)
            if session_result["success"]:
                result["valid"] = True
                result["message"] = "Token 有效"
                return result

            if _is_token_invalid(session_result["status_code"], session_result["data"]):
                token = ""
                account["token"] = ""

        if not token:
            try:
                login_deepseek_via_account(account)
                token = account.get("token", "").strip()
                session_result = _create_session(token)
                if session_result["success"]:
                    result["valid"] = True
                    result["has_token"] = True
                    result["message"] = "登录成功并验证通过"
                else:
                    result["message"] = f"登录成功但验证失败: {session_result['message']}"
            except Exception as e:
                result["valid"] = False
                result["message"] = f"登录失败: {str(e)}"
    except Exception as e:
        result["message"] = f"验证出错: {str(e)}"

    return result


@router.post("/accounts/validate")
async def validate_account(request: Request, _: bool = Depends(verify_admin)):
    """验证单个账号"""
    data = await request.json()
    identifier = data.get("identifier", "").strip()
    
    if not identifier:
        raise HTTPException(status_code=400, detail="需要账号标识（email 或 mobile）")
    
    account = None
    for acc in CONFIG.get("accounts", []):
        if acc.get("email") == identifier or acc.get("mobile") == identifier:
            account = acc
            break
    
    if not account:
        raise HTTPException(status_code=404, detail="账号不存在")
    
    result = await validate_single_account(account)
    
    if result["valid"] and result["has_token"]:
        save_config(CONFIG)
    
    return JSONResponse(content=result)


@router.post("/accounts/validate-all")
async def validate_all_accounts(_: bool = Depends(verify_admin)):
    """批量验证所有账号"""
    accounts = CONFIG.get("accounts", [])
    if not accounts:
        return JSONResponse(content={
            "total": 0, "valid": 0, "invalid": 0, "results": [],
        })
    
    results = []
    valid_count = 0
    
    for acc in accounts:
        result = await validate_single_account(acc)
        results.append(result)
        if result["valid"]:
            valid_count += 1
        await asyncio.sleep(0.5)
    
    save_config(CONFIG)
    
    return JSONResponse(content={
        "total": len(accounts),
        "valid": valid_count,
        "invalid": len(accounts) - valid_count,
        "results": results,
    })


# ----------------------------------------------------------------------
# 账号 API 测试
# ----------------------------------------------------------------------
async def test_account_api(account: dict, model: str = "deepseek-chat", message: str = "") -> dict:
    """测试单个账号的 API 调用能力
    
    如果提供 message，会发送实际请求并返回 AI 回复；
    否则只快速测试创建会话。
    """
    from curl_cffi import requests as cffi_requests
    import time
    
    acc_id = get_account_identifier(account)
    result = {
        "account": acc_id,
        "success": False,
        "response_time": 0,
        "message": "",
        "model": model,
    }
    
    start_time = time.time()
    
    def _is_token_invalid(status_code: int, data: dict) -> bool:
        msg = (data.get("msg") or data.get("message") or "").lower()
        code = data.get("code")
        return status_code in {401, 403} or code in {40001, 40002, 40003} or "token" in msg or "unauthorized" in msg

    def _create_session(token: str) -> dict:
        headers = {**BASE_HEADERS, "authorization": f"Bearer {token}"}
        try:
            session_resp = cffi_requests.post(
                DEEPSEEK_CREATE_SESSION_URL,
                headers=headers,
                json={"agent": "chat"},
                impersonate="safari15_3",
                timeout=15,
            )
        except Exception as e:
            return {"success": False, "message": f"请求异常: {e}", "status_code": 0, "data": {}}

        try:
            session_data = session_resp.json()
        except Exception:
            session_data = {}
        finally:
            session_resp.close()

        if session_resp.status_code == 200 and session_data.get("code") == 0:
            return {
                "success": True,
                "session_id": session_data.get("data", {}).get("biz_data", {}).get("id"),
                "status_code": session_resp.status_code,
                "data": session_data,
            }
        return {
            "success": False,
            "message": session_data.get("msg") or f"HTTP {session_resp.status_code}",
            "status_code": session_resp.status_code,
            "data": session_data,
        }

    try:
        token = account.get("token", "").strip()
        session_result = None
        if token:
            session_result = _create_session(token)

        if not token or (session_result and not session_result["success"] and _is_token_invalid(session_result["status_code"], session_result["data"])):
            try:
                account["token"] = ""
                login_deepseek_via_account(account)
                token = account.get("token", "")
                session_result = _create_session(token)
            except Exception as e:
                result["message"] = f"登录失败: {str(e)}"
                return result

        if not session_result or not session_result["success"]:
            result["message"] = f"创建会话失败: {session_result['message'] if session_result else 'Unknown error'}"
            return result

        session_id = session_result["session_id"]
        headers = {**BASE_HEADERS, "authorization": f"Bearer {token}"}
        
        if not message.strip():
            result["success"] = True
            result["message"] = "API 测试成功（仅会话创建）"
            result["response_time"] = round((time.time() - start_time) * 1000)
            return result
        
        pow_url = "https://chat.deepseek.com/api/v0/chat/create_pow_challenge"
        pow_resp = cffi_requests.post(
            pow_url,
            headers=headers,
            json={"target_path": "/api/v0/chat/completion"},
            timeout=30,
            impersonate="safari15_3",
        )
        
        pow_data = pow_resp.json()
        if pow_data.get("code") != 0:
            result["message"] = f"获取 PoW 失败: {pow_data.get('msg')}"
            return result
        
        challenge = pow_data["data"]["biz_data"]["challenge"]
        try:
            answer = compute_pow_answer(
                challenge["algorithm"],
                challenge["challenge"],
                challenge["salt"],
                challenge.get("difficulty", 144000),
                challenge.get("expire_at", 1680000000),
                challenge["signature"],
                challenge["target_path"],
                WASM_PATH,
            )
        except Exception as e:
            result["message"] = f"PoW 计算失败: {str(e)}"
            return result
        
        pow_dict = {
            "algorithm": challenge["algorithm"],
            "challenge": challenge["challenge"],
            "salt": challenge["salt"],
            "answer": answer,
            "signature": challenge["signature"],
            "target_path": challenge["target_path"],
        }
        pow_str = json.dumps(pow_dict, separators=(",", ":"), ensure_ascii=False)
        pow_header = base64.b64encode(pow_str.encode("utf-8")).decode("utf-8").rstrip()
        
        thinking_enabled, search_enabled = get_model_config(model)
        if thinking_enabled is None:
            thinking_enabled = False
            search_enabled = False
        
        payload = {
            "chat_session_id": session_id,
            "prompt": f"<｜User｜>{message}",
            "ref_file_ids": [],
            "thinking_enabled": thinking_enabled,
            "search_enabled": search_enabled,
        }
        
        completion_headers = {**headers, "x-ds-pow-response": pow_header}
        
        completion_resp = cffi_requests.post(
            DEEPSEEK_COMPLETION_URL,
            headers=completion_headers,
            json=payload,
            impersonate="safari15_3",
            timeout=60,
            stream=True,
        )
        
        if completion_resp.status_code != 200:
            result["message"] = f"请求失败: HTTP {completion_resp.status_code}"
            return result
        
        thinking_parts = []
        content_parts = []
        current_fragment_type = "thinking" if thinking_enabled else "text"
        
        for line in completion_resp.iter_lines():
            if not line:
                continue
            try:
                line_str = line.decode("utf-8")
            except:
                continue
            
            if not line_str.startswith("data:"):
                continue
            
            data_str = line_str[5:].strip()
            if data_str == "[DONE]":
                break
            
            try:
                chunk = json.loads(data_str)
                # 使用共享的解析函数
                contents, is_finished, current_fragment_type = parse_sse_chunk_for_content(
                    chunk, thinking_enabled, current_fragment_type
                )
                
                if is_finished:
                    break
                
                for content, ctype in contents:
                    if ctype == "thinking":
                        thinking_parts.append(content)
                    else:
                        content_parts.append(content)
            except:
                continue
        
        completion_resp.close()
        
        result["success"] = True
        result["response_time"] = round((time.time() - start_time) * 1000)
        result["message"] = "".join(content_parts) or "（无回复内容）"
        if thinking_parts:
            result["thinking"] = "".join(thinking_parts)
        
    except Exception as e:
        result["message"] = f"测试失败: {str(e)}"
    
    return result


@router.post("/accounts/test")
async def test_single_account(request: Request, _: bool = Depends(verify_admin)):
    """测试单个账号的 API 调用"""
    data = await request.json()
    identifier = data.get("identifier", "")
    model = data.get("model", "deepseek-chat")
    message = data.get("message", "")
    
    if not identifier:
        raise HTTPException(status_code=400, detail="需要账号标识（email 或 mobile）")
    
    account = None
    for acc in CONFIG.get("accounts", []):
        if acc.get("email") == identifier or acc.get("mobile") == identifier:
            account = acc
            break
    
    if not account:
        raise HTTPException(status_code=404, detail="账号不存在")
    
    result = await test_account_api(account, model, message)
    save_config(CONFIG)
    
    return JSONResponse(content=result)


@router.post("/accounts/test-all")
async def test_all_accounts(request: Request, _: bool = Depends(verify_admin)):
    """批量测试所有账号的 API 调用"""
    data = await request.json()
    model = data.get("model", "deepseek-chat")
    
    accounts = CONFIG.get("accounts", [])
    if not accounts:
        return JSONResponse(content={
            "total": 0, "success": 0, "failed": 0, "results": [],
        })
    
    results = []
    success_count = 0
    
    for acc in accounts:
        result = await test_account_api(acc, model)
        results.append(result)
        if result["success"]:
            success_count += 1
        await asyncio.sleep(1)
    
    save_config(CONFIG)
    
    return JSONResponse(content={
        "total": len(accounts),
        "success": success_count,
        "failed": len(accounts) - success_count,
        "results": results,
    })


# ----------------------------------------------------------------------
# 批量导入
# ----------------------------------------------------------------------
@router.post("/import")
async def batch_import(request: Request, _: bool = Depends(verify_admin)):
    """批量导入 keys 和 accounts"""
    try:
        data = await request.json()
        imported_keys = 0
        imported_accounts = 0
        
        if "keys" in data:
            for key in data["keys"]:
                if key not in CONFIG.get("keys", []):
                    if "keys" not in CONFIG:
                        CONFIG["keys"] = []
                    CONFIG["keys"].append(key)
                    imported_keys += 1
        
        if "accounts" in data:
            existing_ids = set()
            for acc in CONFIG.get("accounts", []):
                existing_ids.add(acc.get("email", ""))
                existing_ids.add(acc.get("mobile", ""))
            
            for acc in data["accounts"]:
                acc_id = acc.get("email", "") or acc.get("mobile", "")
                if acc_id and acc_id not in existing_ids:
                    if "accounts" not in CONFIG:
                        CONFIG["accounts"] = []
                    CONFIG["accounts"].append(acc)
                    existing_ids.add(acc_id)
                    imported_accounts += 1
        
        init_account_queue()
        save_config(CONFIG)
        
        return JSONResponse(content={
            "success": True,
            "imported_keys": imported_keys,
            "imported_accounts": imported_accounts,
        })
    except json.JSONDecodeError:
        raise HTTPException(status_code=400, detail="无效的 JSON 格式")
    except Exception as e:
        logger.error(f"[batch_import] 错误: {e}")
        raise HTTPException(status_code=500, detail=str(e))

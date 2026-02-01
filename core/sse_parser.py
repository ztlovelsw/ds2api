# -*- coding: utf-8 -*-
"""DeepSeek SSE 流解析模块

这个模块包含解析 DeepSeek SSE 响应的公共逻辑，供 openai.py 和 accounts.py 共用。
"""

from typing import List, Tuple, Optional, Dict, Any

# 跳过的路径模式（状态相关，不是内容）
SKIP_PATTERNS = [
    "quasi_status", "elapsed_secs", "token_usage", 
    "pending_fragment", "conversation_mode",
    "fragments/-1/status", "fragments/-2/status", "fragments/-3/status"
]


def should_skip_chunk(chunk_path: str) -> bool:
    """判断是否应该跳过这个 chunk（状态相关，不是内容）"""
    if chunk_path == "response/search_status":
        return True
    return any(kw in chunk_path for kw in SKIP_PATTERNS)


def is_response_finished(chunk_path: str, v_value: Any) -> bool:
    """判断是否是响应结束信号"""
    return chunk_path == "response/status" and isinstance(v_value, str) and v_value == "FINISHED"


def is_finished_signal(chunk_path: str, v_value: str) -> bool:
    """判断字符串 v_value 是否是结束信号"""
    return v_value == "FINISHED" and (not chunk_path or chunk_path == "status")


def is_search_result(item: dict) -> bool:
    """判断是否是搜索结果项（url/title/snippet）"""
    return "url" in item and "title" in item


def extract_content_from_item(item: dict, default_type: str = "text") -> Optional[Tuple[str, str]]:
    """从包含 content 和 type 的项中提取内容
    
    返回 (content, content_type) 或 None
    """
    if "content" in item and "type" in item:
        inner_type = item.get("type", "").upper()
        content = item.get("content", "")
        if content:
            if inner_type == "THINK" or inner_type == "THINKING":
                return (content, "thinking")
            elif inner_type == "RESPONSE":
                return (content, "text")
            else:
                return (content, default_type)
    return None


def extract_content_recursive(items: List[Dict], default_type: str = "text") -> Optional[List[Tuple[str, str]]]:
    """递归提取列表中的内容
    
    返回 [(content, content_type), ...] 列表，
    如果遇到 FINISHED 信号返回 None
    """
    extracted = []
    for item in items:
        if not isinstance(item, dict):
            continue
        
        item_p = item.get("p", "")
        item_v = item.get("v")
        
        # 跳过搜索结果项
        if is_search_result(item):
            continue
        
        # 只有当 p="status" (精确匹配) 且 v="FINISHED" 才认为是真正结束
        if item_p == "status" and item_v == "FINISHED":
            return None  # 信号结束
        
        # 跳过状态相关
        if should_skip_chunk(item_p):
            continue
        
        # 直接处理包含 content 和 type 的项
        result = extract_content_from_item(item, default_type)
        if result:
            extracted.append(result)
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


def parse_sse_chunk_for_content(chunk: dict, thinking_enabled: bool = False, 
                                 current_fragment_type: str = "thinking") -> Tuple[List[Tuple[str, str]], bool, str]:
    """解析单个 SSE chunk 并提取内容
    
    Args:
        chunk: 解析后的 JSON chunk
        thinking_enabled: 是否启用思考模式
        current_fragment_type: 当前活跃的 fragment 类型 ("thinking" 或 "text")
                              用于处理没有明确路径的空 p 字段内容
    
    Returns:
        (contents, is_finished, new_fragment_type)
        - contents: [(content, content_type), ...] 列表
        - is_finished: 是否是结束信号
        - new_fragment_type: 更新后的 fragment 类型，供下一个 chunk 使用
    """
    if "v" not in chunk:
        return ([], False, current_fragment_type)
    
    v_value = chunk["v"]
    chunk_path = chunk.get("p", "")
    contents = []
    new_fragment_type = current_fragment_type
    
    # 跳过状态相关 chunk
    if should_skip_chunk(chunk_path):
        return ([], False, current_fragment_type)
    
    # 检查是否是真正的响应结束信号
    if is_response_finished(chunk_path, v_value):
        return ([], True, current_fragment_type)
    
    # 检测 fragment 类型变化（来自 APPEND 操作）
    # 格式: {'p': 'response', 'o': 'BATCH', 'v': [{'p': 'fragments', 'o': 'APPEND', 'v': [{'type': 'THINK/RESPONSE', ...}]}]}
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
        ptype = new_fragment_type
    elif not chunk_path:
        # 空路径内容：使用当前活跃的 fragment 类型
        if thinking_enabled:
            ptype = new_fragment_type
        else:
            ptype = "text"
    else:
        ptype = "text"
    
    # 处理字符串值
    if isinstance(v_value, str):
        if is_finished_signal(chunk_path, v_value):
            return ([], True, new_fragment_type)
        if v_value:
            contents.append((v_value, ptype))
    
    # 处理列表值
    elif isinstance(v_value, list):
        result = extract_content_recursive(v_value, ptype)
        if result is None:
            return ([], True, new_fragment_type)
        contents.extend(result)
    
    return (contents, False, new_fragment_type)


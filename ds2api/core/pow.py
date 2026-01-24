from __future__ import annotations

import asyncio
import base64
import ctypes
import hashlib
import struct
import time
from dataclasses import dataclass
from typing import Any

from wasmtime import Engine, Linker, Module, Store

from ds2api.utils.helpers import compact_json_dumps
from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


class PowSolver:
    def __init__(self, wasm_path: str) -> None:
        self._wasm_path = wasm_path
        self._engine = Engine()
        with open(wasm_path, "rb") as f:
            wasm_bytes = f.read()
        self._module = Module(self._engine, wasm_bytes)

    def compute_answer(
        self,
        *,
        algorithm: str,
        challenge_str: str,
        salt: str,
        difficulty: int,
        expire_at: int,
    ) -> int | None:
        if algorithm != "DeepSeekHashV1":
            raise ValueError(f"不支持的算法：{algorithm}")

        prefix = f"{salt}_{expire_at}_"
        store = Store(self._engine)
        linker = Linker(store.engine)
        instance = linker.instantiate(store, self._module)
        exports = instance.exports(store)

        try:
            memory = exports["memory"]
            add_to_stack = exports["__wbindgen_add_to_stack_pointer"]
            alloc = exports["__wbindgen_export_0"]
            wasm_solve = exports["wasm_solve"]
        except KeyError as e:
            raise RuntimeError(f"缺少 wasm 导出函数: {e}")

        def write_memory(offset: int, data: bytes) -> None:
            base_addr = ctypes.cast(memory.data_ptr(store), ctypes.c_void_p).value
            ctypes.memmove(base_addr + offset, data, len(data))

        def read_memory(offset: int, size: int) -> bytes:
            base_addr = ctypes.cast(memory.data_ptr(store), ctypes.c_void_p).value
            return ctypes.string_at(base_addr + offset, size)

        def encode_string(text: str) -> tuple[int, int]:
            data = text.encode("utf-8")
            length = len(data)
            ptr_val = alloc(store, length, 1)
            ptr = int(ptr_val.value) if hasattr(ptr_val, "value") else int(ptr_val)
            write_memory(ptr, data)
            return ptr, length

        retptr = add_to_stack(store, -16)
        ptr_challenge, len_challenge = encode_string(challenge_str)
        ptr_prefix, len_prefix = encode_string(prefix)

        wasm_solve(
            store,
            retptr,
            ptr_challenge,
            len_challenge,
            ptr_prefix,
            len_prefix,
            float(difficulty),
        )

        status_bytes = read_memory(retptr, 4)
        if len(status_bytes) != 4:
            add_to_stack(store, 16)
            raise RuntimeError("读取状态字节失败")

        status = struct.unpack("<i", status_bytes)[0]
        value_bytes = read_memory(retptr + 8, 8)
        if len(value_bytes) != 8:
            add_to_stack(store, 16)
            raise RuntimeError("读取结果字节失败")

        value = struct.unpack("<d", value_bytes)[0]
        add_to_stack(store, 16)

        if status == 0:
            return None

        return int(value)


@dataclass
class _CacheEntry:
    value: int
    expire_at: float


class PowCache:
    def __init__(self, ttl_seconds: int = 60) -> None:
        self._ttl = ttl_seconds
        self._lock = asyncio.Lock()
        self._data: dict[str, _CacheEntry] = {}

    async def get(self, key: str) -> int | None:
        async with self._lock:
            entry = self._data.get(key)
            if not entry:
                return None
            if entry.expire_at < time.time():
                self._data.pop(key, None)
                return None
            return entry.value

    async def set(self, key: str, value: int, *, ttl: int | None = None) -> None:
        async with self._lock:
            ttl_seconds = self._ttl if ttl is None else ttl
            self._data[key] = _CacheEntry(value=value, expire_at=time.time() + ttl_seconds)


class PowService:
    def __init__(self, wasm_path: str, *, cache_ttl_seconds: int = 60) -> None:
        self._solver = PowSolver(wasm_path)
        self._cache = PowCache(ttl_seconds=cache_ttl_seconds)

    @staticmethod
    def _make_cache_key(challenge: dict[str, Any]) -> str:
        challenge_str = str(challenge.get("challenge", ""))
        difficulty = str(challenge.get("difficulty", ""))
        raw = f"{challenge_str}|{difficulty}".encode("utf-8")
        return hashlib.sha256(raw).hexdigest()

    async def solve_encoded_response(self, challenge: dict[str, Any]) -> str | None:
        key = self._make_cache_key(challenge)
        cached = await self._cache.get(key)
        if cached is not None:
            return self._encode_response(challenge, cached)

        algorithm = challenge.get("algorithm")
        challenge_str = challenge.get("challenge")
        salt = challenge.get("salt")
        difficulty = int(challenge.get("difficulty", 144000))
        expire_at = int(challenge.get("expire_at", 0))

        if not all([algorithm, challenge_str, salt, expire_at]):
            logger.warning("[pow] challenge 字段不完整")
            return None

        try:
            answer = await asyncio.to_thread(
                self._solver.compute_answer,
                algorithm=algorithm,
                challenge_str=challenge_str,
                salt=salt,
                difficulty=difficulty,
                expire_at=expire_at,
            )
        except Exception as e:
            logger.error(f"[pow] PoW 答案计算异常: {e}")
            return None

        if answer is None:
            return None

        ttl = 60
        if expire_at:
            ttl = max(1, min(60, expire_at - int(time.time())))
        await self._cache.set(key, answer, ttl=ttl)
        return self._encode_response(challenge, answer)

    @staticmethod
    def _encode_response(challenge: dict[str, Any], answer: int) -> str:
        pow_dict = {
            "algorithm": challenge.get("algorithm"),
            "challenge": challenge.get("challenge"),
            "salt": challenge.get("salt"),
            "answer": answer,
            "signature": challenge.get("signature"),
            "target_path": challenge.get("target_path"),
        }
        pow_str = compact_json_dumps(pow_dict)
        return base64.b64encode(pow_str.encode("utf-8")).decode("utf-8").rstrip()

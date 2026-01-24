from __future__ import annotations

import asyncio
import random
import time
from collections import deque
from dataclasses import dataclass
from typing import Any

from fastapi import HTTPException, Request

from ds2api.config import CONFIG, save_config
from ds2api.core.deepseek import DeepSeekClient
from ds2api.utils.helpers import try_decode_jwt_exp
from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


def get_account_identifier(account: dict[str, Any]) -> str:
    return str(account.get("email", "")).strip() or str(account.get("mobile", "")).strip()


class AccountManager:
    def __init__(self, accounts: list[dict[str, Any]]):
        self._accounts = accounts
        shuffled = accounts[:]
        random.shuffle(shuffled)
        self._available: deque[dict[str, Any]] = deque(shuffled)
        self._in_use: set[str] = set()
        self._lock = asyncio.Lock()
        self._token_obtained_at: dict[str, float] = {}

    async def acquire(self, *, exclude_ids: set[str] | None = None) -> dict[str, Any] | None:
        exclude_ids = exclude_ids or set()
        async with self._lock:
            for _ in range(len(self._available)):
                acc = self._available.popleft()
                acc_id = get_account_identifier(acc)
                if not acc_id or acc_id in exclude_ids or acc_id in self._in_use:
                    self._available.append(acc)
                    continue

                self._in_use.add(acc_id)
                logger.info(f"[accounts] acquire: {acc_id}")
                return acc

            logger.warning("[accounts] 没有可用的账号或所有账号都在使用中")
            return None

    async def release(self, account: dict[str, Any]) -> None:
        acc_id = get_account_identifier(account)
        async with self._lock:
            if acc_id:
                self._in_use.discard(acc_id)
            self._available.append(account)
            if acc_id:
                logger.info(f"[accounts] release: {acc_id}")

    def _token_needs_refresh(self, token: str | None) -> bool:
        if not token:
            return True

        exp = try_decode_jwt_exp(token)
        if exp is not None:
            return exp - int(time.time()) < 300

        return False

    async def ensure_token(self, account: dict[str, Any], deepseek: DeepSeekClient) -> str:
        token = str(account.get("token", "")).strip() or None

        if token and not self._token_needs_refresh(token):
            return token

        email = str(account.get("email", "")).strip() or None
        mobile = str(account.get("mobile", "")).strip() or None
        password = str(account.get("password", "")).strip()

        try:
            new_token = await asyncio.to_thread(
                deepseek.login,
                email=email,
                mobile=mobile,
                password=password,
            )
        except Exception as e:
            logger.error(f"[accounts] 登录失败 {get_account_identifier(account)}: {e}")
            raise HTTPException(status_code=500, detail="Account login failed.")

        account["token"] = new_token
        self._token_obtained_at[get_account_identifier(account)] = time.time()
        save_config(CONFIG)
        return new_token


@dataclass
class AuthContext:
    use_config_token: bool
    token: str
    account: dict[str, Any] | None
    tried_accounts: set[str]
    account_manager: AccountManager | None
    deepseek: DeepSeekClient

    async def rotate_account(self) -> bool:
        if not self.use_config_token or not self.account_manager:
            return False

        if self.account:
            acc_id = get_account_identifier(self.account)
            if acc_id:
                self.tried_accounts.add(acc_id)
            await self.account_manager.release(self.account)

        new_acc = await self.account_manager.acquire(exclude_ids=self.tried_accounts)
        if not new_acc:
            return False

        self.account = new_acc
        self.token = await self.account_manager.ensure_token(new_acc, self.deepseek)
        return True

    async def release(self) -> None:
        if self.use_config_token and self.account_manager and self.account:
            await self.account_manager.release(self.account)


async def determine_mode_and_token(request: Request) -> AuthContext:
    deepseek: DeepSeekClient = request.app.state.deepseek
    account_manager: AccountManager = request.app.state.account_manager
    cfg: dict[str, Any] = request.app.state.config

    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Unauthorized: missing Bearer token.")

    caller_key = auth_header.replace("Bearer ", "", 1).strip()
    config_keys = cfg.get("keys", [])

    if caller_key in config_keys:
        account = await account_manager.acquire()
        if not account:
            raise HTTPException(
                status_code=429, detail="No accounts configured or all accounts are busy."
            )

        token = await account_manager.ensure_token(account, deepseek)

        ctx = AuthContext(
            use_config_token=True,
            token=token,
            account=account,
            tried_accounts=set(),
            account_manager=account_manager,
            deepseek=deepseek,
        )

        request.state.use_config_token = True
        request.state.deepseek_token = token
        request.state.account = account
        request.state.tried_accounts = []
        return ctx

    ctx = AuthContext(
        use_config_token=False,
        token=caller_key,
        account=None,
        tried_accounts=set(),
        account_manager=None,
        deepseek=deepseek,
    )

    request.state.use_config_token = False
    request.state.deepseek_token = caller_key
    return ctx


async def determine_claude_mode_and_token(request: Request) -> AuthContext:
    return await determine_mode_and_token(request)


def get_auth_headers(token: str) -> dict[str, str]:
    from ds2api.core.deepseek import BASE_HEADERS

    return {**BASE_HEADERS, "authorization": f"Bearer {token}"}

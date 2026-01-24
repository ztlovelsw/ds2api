from __future__ import annotations

import time
from typing import Any

from curl_cffi import requests

from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


DEEPSEEK_HOST = "chat.deepseek.com"
DEEPSEEK_LOGIN_URL = f"https://{DEEPSEEK_HOST}/api/v0/users/login"
DEEPSEEK_CREATE_SESSION_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat_session/create"
DEEPSEEK_CREATE_POW_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat/create_pow_challenge"
DEEPSEEK_COMPLETION_URL = f"https://{DEEPSEEK_HOST}/api/v0/chat/completion"

BASE_HEADERS: dict[str, str] = {
    "Host": DEEPSEEK_HOST,
    "User-Agent": "DeepSeek/1.0.13 Android/35",
    "Accept": "application/json",
    "Accept-Encoding": "gzip",
    "Content-Type": "application/json",
    "x-client-platform": "android",
    "x-client-version": "1.3.0-auto-resume",
    "x-client-locale": "zh_CN",
    "accept-charset": "UTF-8",
}


class DeepSeekClient:
    def __init__(
        self,
        *,
        impersonate: str = "safari15_3",
        timeout: int = 30,
    ) -> None:
        self._session = requests.Session()
        self._impersonate = impersonate
        self._timeout = timeout

    def _headers(self, token: str | None = None) -> dict[str, str]:
        if token:
            return {**BASE_HEADERS, "authorization": f"Bearer {token}"}
        return dict(BASE_HEADERS)

    def login(self, *, email: str | None, mobile: str | None, password: str) -> str:
        if not password or (not email and not mobile):
            raise ValueError("账号缺少必要的登录信息（必须提供 email 或 mobile 以及 password）")

        if email:
            payload: dict[str, Any] = {
                "email": email,
                "password": password,
                "device_id": "deepseek_to_api",
                "os": "android",
            }
        else:
            payload = {
                "mobile": mobile,
                "area_code": None,
                "password": password,
                "device_id": "deepseek_to_api",
                "os": "android",
            }

        resp = self._session.post(
            DEEPSEEK_LOGIN_URL,
            headers=self._headers(),
            json=payload,
            impersonate=self._impersonate,
            timeout=self._timeout,
        )
        resp.raise_for_status()
        data = resp.json()

        if (
            data.get("data") is None
            or data["data"].get("biz_data") is None
            or data["data"]["biz_data"].get("user") is None
        ):
            raise RuntimeError("Account login failed: invalid response format")

        token = data["data"]["biz_data"]["user"].get("token")
        if not token:
            raise RuntimeError("Account login failed: missing token")

        return token

    def create_session(self, token: str) -> str | None:
        headers = self._headers(token)
        try:
            resp = self._session.post(
                DEEPSEEK_CREATE_SESSION_URL,
                headers=headers,
                json={"agent": "chat"},
                impersonate=self._impersonate,
                timeout=self._timeout,
            )
        except Exception as e:
            logger.error(f"[create_session] 请求异常: {e}")
            return None

        try:
            data = resp.json()
        except Exception as e:
            logger.error(f"[create_session] JSON解析异常: {e}")
            data = {}

        if resp.status_code == 200 and data.get("code") == 0:
            try:
                return data["data"]["biz_data"]["id"]
            finally:
                resp.close()

        code = data.get("code")
        logger.warning(
            f"[create_session] 创建会话失败, code={code}, msg={data.get('msg')}, status={resp.status_code}"
        )
        resp.close()
        return None

    def create_pow_challenge(self, token: str) -> dict[str, Any] | None:
        headers = self._headers(token)
        try:
            resp = self._session.post(
                DEEPSEEK_CREATE_POW_URL,
                headers=headers,
                json={"target_path": "/api/v0/chat/completion"},
                timeout=self._timeout,
                impersonate=self._impersonate,
            )
        except Exception as e:
            logger.error(f"[create_pow_challenge] 请求异常: {e}")
            return None

        try:
            data = resp.json()
        except Exception as e:
            logger.error(f"[create_pow_challenge] JSON解析异常: {e}")
            data = {}

        if resp.status_code == 200 and data.get("code") == 0:
            try:
                return data["data"]["biz_data"]["challenge"]
            finally:
                resp.close()

        code = data.get("code")
        logger.warning(
            f"[create_pow_challenge] 获取 PoW 失败, code={code}, msg={data.get('msg')}, status={resp.status_code}"
        )
        resp.close()
        return None

    def completion(
        self,
        *,
        headers: dict[str, str],
        payload: dict[str, Any],
        max_attempts: int = 3,
    ) -> requests.Response | None:
        attempts = 0
        while attempts < max_attempts:
            try:
                resp = self._session.post(
                    DEEPSEEK_COMPLETION_URL,
                    headers=headers,
                    json=payload,
                    stream=True,
                    impersonate=self._impersonate,
                )
            except Exception as e:
                logger.warning(f"[completion] 请求异常: {e}")
                time.sleep(1)
                attempts += 1
                continue

            if resp.status_code == 200:
                return resp

            logger.warning(f"[completion] 调用对话接口失败, 状态码: {resp.status_code}")
            resp.close()
            time.sleep(1)
            attempts += 1

        return None

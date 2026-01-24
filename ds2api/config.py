import base64
import json
import os
from dataclasses import dataclass
from typing import Any

from ds2api.utils.logger import get_logger

logger = get_logger(__name__)


BASE_DIR = os.path.dirname(os.path.abspath(os.path.join(os.path.dirname(__file__), os.pardir)))
IS_VERCEL = bool(os.getenv("VERCEL")) or bool(os.getenv("NOW_REGION"))


def resolve_path(env_key: str, default_rel: str) -> str:
    raw = os.getenv(env_key)
    if raw:
        return raw if os.path.isabs(raw) else os.path.join(BASE_DIR, raw)
    return os.path.join(BASE_DIR, default_rel)


@dataclass(frozen=True)
class Settings:
    config_path: str
    templates_dir: str
    wasm_path: str
    keep_alive_timeout: int


settings = Settings(
    config_path=resolve_path("DS2API_CONFIG_PATH", "config.json"),
    templates_dir=resolve_path("DS2API_TEMPLATES_DIR", "templates"),
    wasm_path=resolve_path("DS2API_WASM_PATH", "sha3_wasm_bg.7b9ca65ddd.wasm"),
    keep_alive_timeout=int(os.getenv("DS2API_KEEP_ALIVE_TIMEOUT", "5")),
)


def _load_config_from_env() -> dict[str, Any] | None:
    raw_cfg = os.getenv("DS2API_CONFIG_JSON") or os.getenv("CONFIG_JSON")
    if not raw_cfg:
        return None

    try:
        parsed = json.loads(raw_cfg)
        return parsed if isinstance(parsed, dict) else {}
    except json.JSONDecodeError:
        try:
            decoded = base64.b64decode(raw_cfg).decode("utf-8")
            parsed = json.loads(decoded)
            return parsed if isinstance(parsed, dict) else {}
        except Exception as e:
            logger.warning(f"[load_config] 环境变量配置解析失败: {e}")
            return {}


def load_config() -> dict[str, Any]:
    cfg = _load_config_from_env()
    if cfg is not None:
        return cfg

    try:
        with open(settings.config_path, "r", encoding="utf-8") as f:
            parsed = json.load(f)
            return parsed if isinstance(parsed, dict) else {}
    except Exception as e:
        logger.warning(f"[load_config] 无法读取配置文件({settings.config_path}): {e}")
        return {}


def save_config(cfg: dict[str, Any]) -> None:
    if os.getenv("DS2API_CONFIG_JSON") or os.getenv("CONFIG_JSON"):
        logger.info("[save_config] 配置来自环境变量，跳过写回")
        return

    try:
        with open(settings.config_path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=2)
    except PermissionError as e:
        logger.warning(f"[save_config] 配置文件不可写({settings.config_path}): {e}")
    except Exception as e:
        logger.exception(f"[save_config] 写入 config.json 失败: {e}")


CONFIG: dict[str, Any] = load_config()

if not CONFIG:
    logger.warning(
        "[config] 未加载到有效配置，请提供 config.json（路径可用 DS2API_CONFIG_PATH 指定）或设置环境变量 DS2API_CONFIG_JSON"
    )

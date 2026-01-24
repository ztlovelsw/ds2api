from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from fastapi.templating import Jinja2Templates

from ds2api.config import CONFIG, settings
from ds2api.core.auth import AccountManager
from ds2api.core.deepseek import DeepSeekClient
from ds2api.core.pow import PowService
from ds2api.services import claude, completion, models
from ds2api.utils.logger import configure_logging, get_logger


def create_app() -> FastAPI:
    configure_logging()
    logger = get_logger("ds2api")

    app = FastAPI()

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["GET", "POST", "OPTIONS", "PUT", "DELETE"],
        allow_headers=["Content-Type", "Authorization"],
    )

    templates = Jinja2Templates(directory=settings.templates_dir)

    app.state.settings = settings
    app.state.config = CONFIG
    app.state.deepseek = DeepSeekClient()
    app.state.pow = PowService(settings.wasm_path)
    app.state.account_manager = AccountManager(CONFIG.get("accounts", []))
    app.state.templates = templates

    @app.exception_handler(Exception)
    async def unhandled_exception_handler(request: Request, exc: Exception):
        logger.exception(f"[unhandled_exception] {request.method} {request.url.path}: {exc}")
        return JSONResponse(
            status_code=500,
            content={"error": {"type": "api_error", "message": "Internal Server Error"}},
        )

    app.include_router(models.router)
    app.include_router(completion.router)
    app.include_router(claude.router)

    @app.get("/")
    def index(request: Request):
        return templates.TemplateResponse("welcome.html", {"request": request})

    return app


app = create_app()

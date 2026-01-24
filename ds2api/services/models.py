from fastapi import APIRouter
from fastapi.responses import JSONResponse

router = APIRouter()


@router.get("/v1/models")
def list_models():
    models_list = [
        {
            "id": "deepseek-chat",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-reasoner",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-chat-search",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
        {
            "id": "deepseek-reasoner-search",
            "object": "model",
            "created": 1677610602,
            "owned_by": "deepseek",
            "permission": [],
        },
    ]
    return JSONResponse(content={"object": "list", "data": models_list}, status_code=200)


@router.get("/anthropic/v1/models")
def list_claude_models():
    models_list = [
        {
            "id": "claude-sonnet-4-20250514",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
        {
            "id": "claude-sonnet-4-20250514-fast",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
        {
            "id": "claude-sonnet-4-20250514-slow",
            "object": "model",
            "created": 1715635200,
            "owned_by": "anthropic",
        },
    ]
    return JSONResponse(content={"object": "list", "data": models_list}, status_code=200)

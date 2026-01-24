from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field


class ChatMessage(BaseModel):
    role: Literal["system", "user", "assistant", "tool"] | str
    content: Any = ""


class ChatCompletionRequest(BaseModel):
    model: str
    messages: list[ChatMessage]
    stream: bool = False


class ErrorResponse(BaseModel):
    error: dict[str, Any] = Field(default_factory=dict)

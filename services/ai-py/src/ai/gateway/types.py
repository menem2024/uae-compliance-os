"""ModelGateway contract types (agent-runtime contract v0.2, sections 1 and 2).

Import direction is one-way: ai.runtime imports ai.gateway.types/errors; ai.gateway never
imports ai.runtime.
"""

from __future__ import annotations

import hashlib
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Final, Literal, Protocol

from pydantic import BaseModel

ModelId = Literal["claude-opus-5-5", "claude-sonnet-5", "claude-haiku-4-5-20251001"]
OPUS: Final[ModelId] = "claude-opus-5-5"
SONNET: Final[ModelId] = "claude-sonnet-5"
HAIKU: Final[ModelId] = "claude-haiku-4-5-20251001"
MODEL_IDS: Final[tuple[str, ...]] = (OPUS, SONNET, HAIKU)


@dataclass(frozen=True, slots=True)
class ToolSpec:
    name: str  # ^[a-z][a-z0-9_]{1,63}$
    version: int
    description: str  # shown to the model
    input_model: type[BaseModel]
    side_effects: Literal["none", "read", "propose"]
    timeout_s: float = 30.0


@dataclass(frozen=True, slots=True)
class TextPart:
    text: str


def _check_sha(data: bytes, sha256: str) -> None:
    if hashlib.sha256(data).hexdigest() != sha256:
        raise ValueError("sha256 does not match data")


@dataclass(frozen=True, slots=True)
class DocumentPart:
    data: bytes
    sha256: str  # lowercase hex of data; the cache key uses this, never the bytes
    media_type: Literal["application/pdf"] = "application/pdf"

    def __post_init__(self) -> None:
        _check_sha(self.data, self.sha256)

    @classmethod
    def of(cls, data: bytes) -> DocumentPart:
        return cls(data=data, sha256=hashlib.sha256(data).hexdigest())


@dataclass(frozen=True, slots=True)
class ImagePart:
    data: bytes
    sha256: str
    media_type: Literal["image/png", "image/jpeg", "image/webp"]

    def __post_init__(self) -> None:
        _check_sha(self.data, self.sha256)

    @classmethod
    def of(cls, data: bytes, media_type: Literal["image/png", "image/jpeg", "image/webp"]) -> ImagePart:
        return cls(data=data, sha256=hashlib.sha256(data).hexdigest(), media_type=media_type)


@dataclass(frozen=True, slots=True)
class ToolUsePart:
    id: str
    name: str
    input: Mapping[str, object]


@dataclass(frozen=True, slots=True)
class ToolResultPart:
    tool_use_id: str
    content: str
    is_error: bool = False


type Part = TextPart | DocumentPart | ImagePart | ToolUsePart | ToolResultPart


@dataclass(frozen=True, slots=True)
class Message:
    role: Literal["user", "assistant"]
    parts: tuple[Part, ...]


@dataclass(frozen=True, slots=True)
class RequestMeta:
    firm_id: str
    run_id: str
    step_id: str


@dataclass(frozen=True, slots=True)
class ModelRequest:
    model: ModelId
    prompt_id: str  # "<agent>.<task>", e.g. "extraction.invoice"
    prompt_version: int  # bump on any prompt, schema or model change
    system: str
    messages: tuple[Message, ...]
    output_model: type[BaseModel] | None = None
    tools: tuple[ToolSpec, ...] = ()
    max_tokens: int = 4096
    effort: Literal["low", "medium", "high"] | None = None
    cacheable: bool = True  # gateway response cache
    meta: RequestMeta | None = None  # set by AgentContext.complete(); agents leave it None


@dataclass(frozen=True, slots=True)
class ToolCall:
    id: str
    name: str
    input: Mapping[str, object]


@dataclass(frozen=True, slots=True)
class Usage:
    model: str = ""
    prompt_id: str = ""
    prompt_version: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_input_tokens: int = 0
    cache_creation_input_tokens: int = 0
    cost_micro_usd: int = 0
    response_cache_hit: bool = False
    llm_calls: int = 0  # provider calls actually made (0 on a cache hit)


@dataclass(frozen=True, slots=True)
class ModelResponse:
    model: str
    text: str  # raw text (JSON text when output_model is set)
    parsed: BaseModel | None
    tool_calls: tuple[ToolCall, ...]
    stop_reason: str  # end_turn | tool_use | max_tokens | refusal | ...
    usage: Usage
    latency_ms: int
    cache_key: str  # sha256 hex
    provider_request_id: str = ""  # Message._request_id; "" on cache hit / fake


class ModelGateway(Protocol):
    async def complete(self, req: ModelRequest) -> ModelResponse: ...


__all__ = [
    "HAIKU", "MODEL_IDS", "OPUS", "SONNET", "DocumentPart", "ImagePart", "Message", "ModelGateway",
    "ModelId", "ModelRequest", "ModelResponse", "Part", "RequestMeta", "TextPart", "ToolCall",
    "ToolResultPart", "ToolSpec", "ToolUsePart", "Usage",
]

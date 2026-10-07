"""The source document an agent reads: bytes plus media type, turned into gateway parts (PDF or image)."""

from __future__ import annotations

import hashlib
from typing import Literal, Self

from pydantic import BaseModel, ConfigDict, model_validator

from ai.gateway.types import DocumentPart, ImagePart, Part

type MediaType = Literal["application/pdf", "image/png", "image/jpeg", "image/webp"]
MEDIA_TYPES: tuple[str, ...] = ("application/pdf", "image/png", "image/jpeg", "image/webp")


class SourceDocument(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid")

    data: bytes
    media_type: MediaType
    sha256: str

    @model_validator(mode="after")
    def _sha_matches(self) -> Self:
        if hashlib.sha256(self.data).hexdigest() != self.sha256:
            raise ValueError("sha256 does not match data")
        return self

    @classmethod
    def of(cls, data: bytes, media_type: MediaType) -> SourceDocument:
        return cls(data=data, media_type=media_type, sha256=hashlib.sha256(data).hexdigest())

    def parts(self) -> tuple[Part, ...]:
        """One binary part; the cache key uses its sha256, never the bytes (contract section 2)."""
        if self.media_type == "application/pdf":
            return (DocumentPart(data=self.data, sha256=self.sha256),)
        return (ImagePart(data=self.data, sha256=self.sha256, media_type=self.media_type),)

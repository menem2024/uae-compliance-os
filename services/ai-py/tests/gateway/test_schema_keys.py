import dataclasses
import json

from pydantic import BaseModel, ConfigDict, Field

from ai.gateway.keys import cache_key
from ai.gateway.schema import strict_schema
from ai.gateway.types import SONNET, DocumentPart, Message, ModelRequest, RequestMeta, TextPart


class Line(BaseModel):
    model_config = ConfigDict(extra="forbid")
    name: str = Field(default="", max_length=200, title="Name")
    qty: str = Field(default="", pattern=r"^\d+$")


class Doc(BaseModel):
    model_config = ConfigDict(extra="forbid")
    number: str = ""
    lines: list[Line] = []
    kind: str = Field(default="invoice", json_schema_extra={"enum": ["invoice", "other"]})


def walk(n):
    if isinstance(n, dict):
        yield n
        for v in n.values():
            yield from walk(v)
    elif isinstance(n, list):
        for v in n:
            yield from walk(v)


def test_strict_schema_inlines_and_strips():
    s = strict_schema(Doc)
    text = json.dumps(s)
    assert "$ref" not in text and "$defs" not in text
    for node in walk(s):
        for bad in ("pattern", "maxLength", "default", "title"):
            assert bad not in node
        if node.get("type") == "object":
            assert node["additionalProperties"] is False
            assert node["required"] == list(node["properties"])
    assert s["properties"]["lines"]["items"]["required"] == ["name", "qty"]


def req(firm="f1", data=b"%PDF-1", text="hi"):
    return ModelRequest(model=SONNET, prompt_id="extraction.invoice", prompt_version=1, system="sys",
                        messages=(Message("user", (DocumentPart.of(data), TextPart(text))),),
                        output_model=Doc, meta=RequestMeta(firm, "r1", "s1"))


def test_cache_key_stable_and_sensitive():
    assert cache_key(req()) == cache_key(req())
    assert cache_key(req()) != cache_key(req(firm="f2"))
    assert cache_key(req()) != cache_key(req(data=b"%PDF-2"))
    assert cache_key(req()) != cache_key(req(text="ho"))
    # run and step ids never enter the key (a redelivered run hits the cache)
    a = req()
    assert cache_key(a) == cache_key(dataclasses.replace(a, meta=RequestMeta("f1", "r2", "s2")))
    # the prompt version does (rule 8: a prompt change invalidates cache and recordings)
    assert cache_key(a) != cache_key(dataclasses.replace(a, prompt_version=2))

"""Gateway cache and recording key (contract section 2)."""

from ai.canonical import canonical_json, sha256_hex
from ai.gateway.types import (
    DocumentPart,
    ImagePart,
    ModelRequest,
    Part,
    TextPart,
    ToolResultPart,
    ToolUsePart,
)

KEY_VERSION = 1


def part_descriptor(p: Part) -> list[object]:
    match p:
        case TextPart(text=text):
            return ["text", text]
        case DocumentPart(sha256=sha):
            return ["document", sha]
        case ImagePart(media_type=mt, sha256=sha):
            return ["image", mt, sha]
        case ToolUsePart(id=id_, name=name, input=inp):
            return ["tool_use", id_, name, dict(inp)]
        case ToolResultPart(tool_use_id=tid, content=content, is_error=err):
            return ["tool_result", tid, content, err]
    raise TypeError(f"unknown part {type(p).__name__}")


def key_object(req: ModelRequest) -> dict[str, object]:
    return {
        "v": KEY_VERSION,
        "firm_id": req.meta.firm_id if req.meta else "",
        "model": req.model,
        "prompt_id": req.prompt_id,
        "prompt_version": req.prompt_version,
        "system_sha256": sha256_hex(req.system),
        "schema": req.output_model.model_json_schema() if req.output_model else None,
        "tools": [f"{t.name}@{t.version}" for t in req.tools],
        "max_tokens": req.max_tokens,
        "effort": req.effort,
        "messages": [[m.role, [part_descriptor(p) for p in m.parts]] for m in req.messages],
    }


def cache_key(req: ModelRequest) -> str:
    return sha256_hex(canonical_json(key_object(req)))

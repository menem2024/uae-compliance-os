"""Structured-Outputs-safe JSON schema from a pydantic model (contract section 2)."""

from typing import Any

from pydantic import BaseModel

_STRIP = frozenset(
    {"pattern", "format", "minLength", "maxLength", "minimum", "maximum", "exclusiveMinimum",
     "exclusiveMaximum", "multipleOf", "minItems", "maxItems", "default", "title", "examples"}
)


def _resolve(node: Any, defs: dict[str, Any], depth: int = 0) -> Any:
    if depth > 64:
        raise ValueError("schema too deep or recursive")
    if isinstance(node, list):
        return [_resolve(n, defs, depth + 1) for n in node]
    if not isinstance(node, dict):
        return node
    if "$ref" in node:
        name = node["$ref"].rsplit("/", 1)[-1]
        merged = {**defs[name], **{k: v for k, v in node.items() if k != "$ref"}}
        return _resolve(merged, defs, depth + 1)
    out: dict[str, Any] = {}
    for k, v in node.items():
        if k in _STRIP or k == "$defs":
            continue
        if k == "properties":
            out[k] = {pk: _resolve(pv, defs, depth + 1) for pk, pv in v.items()}
        else:
            out[k] = _resolve(v, defs, depth + 1)
    if out.get("type") == "object" or "properties" in out:
        props = out.setdefault("properties", {})
        out["type"] = "object"
        out["additionalProperties"] = False
        out["required"] = list(props)
    return out


def strict_schema(model: type[BaseModel]) -> dict[str, Any]:
    raw = model.model_json_schema()
    return _resolve(raw, raw.get("$defs", {}))

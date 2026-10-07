"""Canonical JSON and hashing shared by cache keys, recordings and proposal ids."""

import hashlib
import json


def canonical_json(obj: object) -> str:
    """Deterministic JSON: sorted keys, no spaces, UTF-8 kept (contract section 2)."""
    return json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def sha256_hex(data: bytes | str) -> str:
    if isinstance(data, str):
        data = data.encode("utf-8")
    return hashlib.sha256(data).hexdigest()

"""Field paths on the canonical Invoice (canonical-invoice contract section 12).

A mirror of `services/api-go/internal/fieldpath`: `segment ("." segment)*`, `segment = proto_field_name
("[" index "]")?`, 0-based indices, no `invoice.` prefix, and a path always ends on a string or bool field.
Pure: no I/O. `AGENT_FORBIDDEN` is the same list api-go enforces; the agent applies it before it proposes
(api-go enforces it again when a human accepts).
"""

from __future__ import annotations

import re
from collections.abc import Iterable

from google.protobuf.descriptor import Descriptor, FieldDescriptor

from ai.gen.compliance.v1 import invoice_pb2

AGENT_FORBIDDEN: frozenset[str] = frozenset({
    "invoice_number", "uuid", "issue_date", "seller_trn", "buyer_trn",
    "seller.tax_registration_identifier", "seller.electronic_address.id", "buyer.electronic_address.id",
    "seller.legal_registration.id", "buyer.legal_registration.id", "principal_id", "beneficiary_id",
    "tax_representative.vat_identifier",
})

_SEGMENT = re.compile(r"^([a-z][a-z0-9_]*)(?:\[([0-9]+)\])?$")
_STRING = FieldDescriptor.TYPE_STRING
_BOOL = FieldDescriptor.TYPE_BOOL
_MESSAGE = FieldDescriptor.TYPE_MESSAGE

type Segments = list[tuple[str, int | None]]


def is_forbidden(path: str) -> bool:
    return path in AGENT_FORBIDDEN


def _parse(path: str) -> Segments:
    if not path:
        raise ValueError("empty path")
    out: Segments = []
    for part in path.split("."):
        m = _SEGMENT.match(part)
        if m is None:
            raise ValueError(f"bad segment {part!r}")
        out.append((m.group(1), int(m.group(2)) if m.group(2) is not None else None))
    return out


def _is_map(fd: FieldDescriptor) -> bool:
    return fd.type == _MESSAGE and fd.message_type.GetOptions().map_entry


def _leaf(segs: Segments, path: str) -> FieldDescriptor:
    """The leaf field of `path`, or ValueError when it breaks the grammar or the Invoice descriptor."""
    md: Descriptor = invoice_pb2.Invoice.DESCRIPTOR
    for i, (name, index) in enumerate(segs):
        fd = md.fields_by_name.get(name)
        if fd is None:
            raise ValueError(f"no field {name!r} in {path!r}")
        if i == len(segs) - 1:
            if fd.is_repeated or index is not None or fd.type not in (_STRING, _BOOL):
                raise ValueError(f"{path!r} does not end on a string or bool field")
            return fd
        if fd.type != _MESSAGE or _is_map(fd):
            raise ValueError(f"{name!r} is not a message in {path!r}")
        if fd.is_repeated != (index is not None):
            raise ValueError(f"index use on {name!r} in {path!r}")
        md = fd.message_type
    raise ValueError(path)


def is_valid_path(path: str) -> bool:
    try:
        _leaf(_parse(path), path)
    except ValueError:
        return False
    return True


def is_bool_path(path: str) -> bool:
    try:
        return _leaf(_parse(path), path).type == _BOOL
    except ValueError:
        return False


def get_value(inv: invoice_pb2.Invoice, path: str) -> str:
    """The value at `path`: "" when it (or a parent) is absent; "true"/"false" for a bool on an existing
    message. Never mutates `inv`."""
    segs = _parse(path)
    fd = _leaf(segs, path)
    msg = inv
    for name, index in segs[:-1]:
        if index is not None:
            items = getattr(msg, name)
            if index >= len(items):
                return ""
            msg = items[index]
        else:
            if not msg.HasField(name):
                return ""
            msg = getattr(msg, name)
    value = getattr(msg, segs[-1][0])
    return ("true" if value else "false") if fd.type == _BOOL else str(value)


def set_value(inv: invoice_pb2.Invoice, path: str, value: str) -> None:
    """Sets `path` to `value` in place (a list index equal to the list length appends an element).
    ValueError on a bad path, a bool given something other than true/false, or an index beyond the list."""
    segs = _parse(path)
    fd = _leaf(segs, path)
    if fd.type == _BOOL and value not in ("true", "false"):
        raise ValueError(f"{path!r} is a bool, {value!r} is not true or false")
    msg = inv
    for name, index in segs[:-1]:
        if index is None:
            msg = getattr(msg, name)
            continue
        items = getattr(msg, name)
        if index < len(items):
            msg = items[index]
        elif index == len(items):
            msg = items.add()
        else:
            raise ValueError(f"index {index} beyond length {len(items)} in {path!r}")
    setattr(msg, segs[-1][0], (value == "true") if fd.type == _BOOL else value)


def apply_changes(inv: invoice_pb2.Invoice, changes: Iterable[tuple[str, str]]) -> invoice_pb2.Invoice:
    """A copy of `inv` with every (path, new_value) set; `inv` is left unchanged. ValueError on a bad change."""
    out = invoice_pb2.Invoice()
    out.CopyFrom(inv)
    for path, value in changes:
        set_value(out, path, value)
    return out


def clone(inv: invoice_pb2.Invoice) -> invoice_pb2.Invoice:
    out = invoice_pb2.Invoice()
    out.CopyFrom(inv)
    return out

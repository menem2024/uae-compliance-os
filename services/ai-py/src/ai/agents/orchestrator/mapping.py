"""Descriptor-guarded ExtractedInvoice -> compliance.v1.Invoice mapping (contract section 4.2, gate G1).

ExtractedInvoice field names are the canonical-invoice v0.2 proto field names, so the mapping is a generic walk:
a field is copied only when the target message descriptor has a field of the same name and a compatible type.
With today's Phase 0 `invoice.proto` (7 flat fields) the other field-set paths are reported in `dropped`; after
Track C regenerates `invoice.proto` v2 the same code fills every field, with no code change.
"""

from __future__ import annotations

from dataclasses import dataclass

from google.protobuf.descriptor import FieldDescriptor
from google.protobuf.message import Message
from pydantic import BaseModel

from ai.agents.extraction.schema import ExtractedInvoice
from ai.gen.compliance.v1 import invoice_pb2

HAS_V2: bool = "lines" in invoice_pb2.Invoice.DESCRIPTOR.fields_by_name


@dataclass(frozen=True, slots=True)
class MappedInvoice:
    message: Message
    dropped: tuple[str, ...]  # non-empty field-set paths the target proto cannot hold (yet)


def _non_empty(value: object) -> bool:
    if isinstance(value, BaseModel):
        return any(_non_empty(getattr(value, n)) for n in type(value).model_fields)
    if isinstance(value, list):
        return any(_non_empty(v) for v in value)
    return bool(value)


def _copy(src: BaseModel, dst: Message, prefix: str, dropped: list[str]) -> None:
    fields = dst.DESCRIPTOR.fields_by_name
    for name in type(src).model_fields:
        value = getattr(src, name)
        if not _non_empty(value):
            continue
        path = f"{prefix}{name}"
        fd = fields.get(name)
        if isinstance(value, list):
            if fd is None or not fd.is_repeated or fd.message_type is None:
                dropped.append(path)
                continue
            container = getattr(dst, name)
            for i, item in enumerate(value):
                _copy(item, container.add(), f"{path}[{i}].", dropped)
        elif isinstance(value, BaseModel):
            if fd is None or fd.is_repeated or fd.message_type is None:
                dropped.append(path)
                continue
            _copy(value, getattr(dst, name), f"{path}.", dropped)
        else:
            if fd is None or fd.is_repeated or fd.type != FieldDescriptor.TYPE_STRING:
                dropped.append(path)
                continue
            setattr(dst, name, value)


def map_invoice(inv: ExtractedInvoice, cls: type[Message] | None = None) -> MappedInvoice:
    """Copies every non-empty field `cls` (default `compliance.v1.Invoice`) can hold."""
    msg = (cls or invoice_pb2.Invoice)()
    dropped: list[str] = []
    _copy(inv, msg, "", dropped)
    return MappedInvoice(message=msg, dropped=tuple(dropped))

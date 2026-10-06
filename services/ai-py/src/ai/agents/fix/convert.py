"""Protobuf <-> canonical JSON text for the Fix agent (proto field names, deterministic key order)."""

from __future__ import annotations

import json
from collections.abc import Mapping
from typing import Any

from google.protobuf import json_format

from ai.gen.compliance.v1 import invoice_pb2, validator_pb2


def _dump(msg: Any) -> str:
    d = json_format.MessageToDict(msg, preserving_proto_field_name=True)
    return json.dumps(d, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def invoice_json(inv: invoice_pb2.Invoice) -> str:
    return _dump(inv)


def invoice_from_dict(d: Mapping[str, Any]) -> invoice_pb2.Invoice:
    return json_format.ParseDict(dict(d), invoice_pb2.Invoice())


def run_json(run: validator_pb2.ValidationRun) -> str:
    return _dump(run)


def run_from_json(text: str) -> validator_pb2.ValidationRun:
    return json_format.Parse(text, validator_pb2.ValidationRun())


def is_error(issue: validator_pb2.ValidationIssue) -> bool:
    return issue.severity == validator_pb2.SEVERITY_ERROR


def error_issues(run: validator_pb2.ValidationRun) -> tuple[validator_pb2.ValidationIssue, ...]:
    return tuple(i for i in run.issues if is_error(i))

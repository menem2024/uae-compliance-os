"""OTel text-map carrier for NATS message headers (dict[str, str])."""

from opentelemetry.propagators.textmap import Getter, Setter


class _Getter(Getter[dict]):
    def get(self, carrier: dict, key: str) -> list[str] | None:
        v = carrier.get(key)
        return [v] if v is not None else None

    def keys(self, carrier: dict) -> list[str]:
        return list(carrier.keys())


class _Setter(Setter[dict]):
    def set(self, carrier: dict, key: str, value: str) -> None:
        carrier[key] = value


getter = _Getter()
setter = _Setter()

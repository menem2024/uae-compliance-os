# ghcr.io/astral-sh/uv:0.12-python3.14-bookworm-slim does not exist yet (no
# such combined tag published); use python:3.14-slim plus the standalone uv
# binary instead, per the story fallback.
FROM python:3.14-slim
COPY --from=ghcr.io/astral-sh/uv:0.12 /uv /bin/uv
WORKDIR /app
COPY services/ai-py/pyproject.toml services/ai-py/uv.lock services/ai-py/README.md ./
RUN uv sync --frozen --no-dev --no-install-project
COPY services/ai-py/src ./src
RUN uv sync --frozen --no-dev
EXPOSE 8081
# Run the venv's python directly instead of `uv run`: uid 65532 has no passwd
# entry (so $HOME=/), and `uv run` tries to create/write a cache under
# $HOME/.cache/uv at every invocation, which fails with a read-only root fs
# or no write access as a non-root user. Invoking the venv interpreter
# directly needs no uv invocation, hence no cache, at container runtime.
ENV PATH=/app/.venv/bin:$PATH
USER 65532
CMD ["/app/.venv/bin/python", "-m", "ai"]

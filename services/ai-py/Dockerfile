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
USER 65532
CMD ["uv", "run", "--no-sync", "python", "-m", "ai"]

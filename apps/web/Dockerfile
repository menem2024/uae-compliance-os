# syntax=docker/dockerfile:1.7
# Compliance OS web tier (Next.js standalone). Build context = repo root:
#   docker build -f apps/web/Dockerfile -t web:dev .
ARG NODE_IMAGE=node:24.21.0-alpine3.24

# ---- deps: install exactly what the lockfile pins ----
FROM ${NODE_IMAGE} AS deps
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

# ---- build: next build with output "standalone" ----
FROM ${NODE_IMAGE} AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=deps /app/node_modules ./node_modules
COPY apps/web/ ./
RUN npm run build

# ---- runtime: only the standalone server + static assets, non-root ----
FROM ${NODE_IMAGE} AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=3000 \
    # Next's own SIGTERM handler exits 143; src/lib/server/shutdown.ts takes over instead
    # (flush OTel, exit 0) so `docker stop` is clean without compose `init: true`.
    NEXT_MANUAL_SIG_HANDLE=true
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
USER node
EXPOSE 3000
CMD ["node", "server.js"]

FROM rust:1.98-bookworm AS build
WORKDIR /src
COPY proto /proto
COPY services/validator-rs /src
RUN sed -i 's#\.\./\.\./proto#/proto#' build.rs && cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/target/release/validator-rs /usr/local/bin/validator-rs
EXPOSE 50051
USER 65532
ENTRYPOINT ["validator-rs"]

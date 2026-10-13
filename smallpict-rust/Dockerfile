FROM rust:1.77-slim as builder

WORKDIR /app
COPY . .
RUN cargo build --release

FROM debian:bookworm-slim
WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libssl-dev \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/target/release/smallpict-rust /app/smallpict-rust

EXPOSE 8007

CMD ["/app/smallpict-rust"]

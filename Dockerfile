# ==============================================================================
#  NivaroOS Container Platform - Multi-Stage Production Dockerfile
# ==============================================================================

# Stage 1: Build Frontend UI
FROM node:20-alpine AS ui-builder
WORKDIR /app/ui
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY ui/package.json ui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY ui/ ./
RUN pnpm run build

# Stage 2: Build Backend Go Microservices
FROM golang:1.24-bookworm AS go-builder
WORKDIR /build
ENV GOTOOLCHAIN=auto

# Copy service sources
COPY services/ ./services/
COPY cli/ ./cli/

# Compile all microservices
RUN mkdir -p /build/bin && \
    (cd services/core && GOWORK=off go build -o /build/bin/nivaroos .) && \
    (cd services/gateway && GOWORK=off go build -o /build/bin/nivaroos-gateway .) && \
    (cd services/message-bus && GOWORK=off go build -o /build/bin/nivaroos-message-bus .) && \
    (cd services/app-management && GOWORK=off go build -o /build/bin/nivaroos-app-management .) && \
    (cd services/local-storage && GOWORK=off go build -o /build/bin/nivaroos-local-storage .) && \
    (cd services/user && GOWORK=off go build -o /build/bin/nivaroos-user-service .) && \
    (cd services/gpu-sidecar && GOWORK=off go build -o /build/bin/nivaroos-gpu-sidecar .) && \
    (cd cli && GOWORK=off go build -o /build/bin/nivaroos-cli .)

# Stage 3: Final Runtime Image
FROM debian:bookworm-slim

LABEL org.opencontainers.image.title="NivaroOS" \
      org.opencontainers.image.description="A modern, self-hosted personal cloud OS and container platform" \
      org.opencontainers.image.url="https://github.com/F-e-n-y-x/NivaroOS" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV DEBIAN_FRONTEND=noninteractive \
    PORT=80

# Install runtime utilities & Docker CLI
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    tar \
    util-linux \
    procps \
    smartmontools \
    pciutils \
    iproute2 \
    tini \
    docker.io \
    && rm -rf /var/lib/apt/lists/*

# Create application directories
RUN mkdir -p /var/run/nivaroos /var/log/nivaroos /var/lib/nivaroos/www /etc/nivaroos \
             /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery

# Copy compiled backend binaries
COPY --from=go-builder /build/bin/* /usr/bin/
RUN ln -sf /usr/bin/nivaroos-cli /usr/local/bin/nivaroos && \
    ln -sf /usr/bin/nivaroos-cli /usr/bin/casaos-cli

# Copy compiled frontend assets
COPY --from=ui-builder /app/ui/build/sysroot/var/lib/nivaroos/www/ /var/lib/nivaroos/www/

# Copy default config and entrypoint script
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 80

VOLUME ["/DATA", "/etc/nivaroos", "/var/run/docker.sock"]

ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]

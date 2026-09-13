# syntax=docker/dockerfile:1.7
# Build stages run on the build host; Go cross-compiles for TARGETARCH, so
# an arm64 image does not need emulation.

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
RUN corepack enable && corepack prepare pnpm@9 --activate
WORKDIR /src
COPY web/admin/package.json web/admin/pnpm-lock.yaml web/admin/
COPY web/portal/package.json web/portal/pnpm-lock.yaml web/portal/
COPY web/site/package.json web/site/pnpm-lock.yaml web/site/
COPY web/probe/package.json web/probe/pnpm-lock.yaml web/probe/
RUN cd web/admin && pnpm install --frozen-lockfile && cd ../portal && pnpm install --frozen-lockfile && cd ../site && pnpm install --frozen-lockfile && cd ../probe && pnpm install --frozen-lockfile
COPY web/ web/
RUN cd web/admin && pnpm build && cd ../portal && pnpm build && cd ../site && pnpm build && cd ../probe && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=docker
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/admin/dist web/admin/dist
COPY --from=web /src/web/portal/dist web/portal/dist
COPY --from=web /src/web/site/dist web/site/dist
COPY --from=web /src/web/probe/dist web/probe/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/captain ./cmd/captain

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 1000 captain
# The self-updater points at `docker compose pull` instead of swapping the binary.
ENV IN_CONTAINER=1
COPY --from=build /out/captain /usr/local/bin/captain
COPY config.example.yaml /etc/captain/config.example.yaml
COPY deploy/config.docker.yaml /etc/captain/config.yaml
RUN mkdir -p /var/lib/captain && chown captain:captain /var/lib/captain
USER captain
VOLUME /var/lib/captain
EXPOSE 8080
ENTRYPOINT ["captain"]
CMD ["serve", "-c", "/etc/captain/config.yaml"]

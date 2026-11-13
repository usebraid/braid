# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
FROM --platform=$BUILDPLATFORM node:26.13.0-alpine@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/usebraid/braid/internal/buildinfo.Version=$VERSION -X github.com/usebraid/braid/internal/buildinfo.Commit=$COMMIT" -o /braid-server ./cmd/braid-server

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG SOURCE_URL=https://github.com/usebraid/braid
ARG VERSION=dev
ARG COMMIT=
LABEL org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$COMMIT \
      org.opencontainers.image.licenses=AGPL-3.0-or-later \
      org.opencontainers.image.title=braid-server
COPY --from=build /braid-server /braid-server
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/braid-server"]

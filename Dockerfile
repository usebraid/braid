# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/usebraid/braid/internal/buildinfo.Version=$VERSION -X github.com/usebraid/braid/internal/buildinfo.Commit=$COMMIT" -o /braid-server ./cmd/braid-server

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /braid-server /braid-server
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/braid-server"]

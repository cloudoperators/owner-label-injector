# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.24 as builder

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum

COPY main.go main.go
COPY api/ api/
COPY labeller/ labeller/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go \
  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o labeller/labeller labeller/main.go

ARG LINKERD_AWAIT_VERSION=v0.2.8
RUN curl -sSLo /tmp/linkerd-await https://github.com/linkerd/linkerd-await/releases/download/release%2F${LINKERD_AWAIT_VERSION}/linkerd-await-${LINKERD_AWAIT_VERSION}-amd64 && \
    chmod 755 /tmp/linkerd-await


FROM gcr.io/distroless/static:nonroot

LABEL source_repository="https://github.com/cloudoperators/owner-label-injector"

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/labeller/labeller .
COPY --from=builder /tmp/linkerd-await /linkerd-await

USER 65532:65532

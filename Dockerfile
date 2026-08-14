# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.26.5 as builder

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum

COPY main.go main.go
COPY api/ api/
COPY labeller/ labeller/
COPY label-remover/ label-remover/
COPY internal/ internal/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager main.go \
  && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o labeller/labeller labeller/main.go \
  && GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o label-remover/label-remover label-remover/main.go

ARG LINKERD_AWAIT_VERSION=v0.2.8
RUN curl -sSLo /tmp/linkerd-await https://github.com/linkerd/linkerd-await/releases/download/release%2F${LINKERD_AWAIT_VERSION}/linkerd-await-${LINKERD_AWAIT_VERSION}-amd64 && \
    chmod 755 /tmp/linkerd-await


FROM --platform=${BUILDPLATFORM:-linux/amd64} gcr.io/distroless/static:nonroot

LABEL source_repository="https://github.com/cloudoperators/owner-label-injector"

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/labeller/labeller .
COPY --from=builder /workspace/label-remover/label-remover .
COPY --from=builder /tmp/linkerd-await /linkerd-await

USER 65532:65532

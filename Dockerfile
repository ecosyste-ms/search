ARG GO_IMAGE=golang:1.27.3-trixie@sha256:1896654cfc5c2b5ad74c07f29cb36f5d2d1cbed42e3054070fbfe444e521e83c
ARG RUNTIME_IMAGE=debian:trixie@sha256:913f6706df59a68922d1dd08f78c2476560a8d367897200a6005b00e5f67c2d5
FROM ${GO_IMAGE} AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o /search ./cmd/search

FROM ${RUNTIME_IMAGE}
COPY --from=builder /search /usr/local/bin/search
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENV GOMEMLIMIT=512MiB
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/search"]
CMD ["explore", "-index", "/snapshots/current/snapshot/index", "-catalog", "/snapshots/current/snapshot", "-addr", "0.0.0.0:8080"]

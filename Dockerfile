# Build: a static binary with the web interface embedded.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /musaarr .

# Run: distroless, no shell, non-root by default (compose sets user 3000:3000).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /musaarr /musaarr
ENV DATA_DIR=/data
VOLUME /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/musaarr", "-healthcheck"]
ENTRYPOINT ["/musaarr"]

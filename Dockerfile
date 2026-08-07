# Usage: docker run --rm -v "$PWD:/repo:ro" ghcr.io/mithrilbytes/overwater
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/MithrilBytes/overwater/internal/cli.buildVersion=${VERSION}" \
      -o /out/overwater ./cmd/overwater

# distroless static, not scratch: the binary is static and carries the
# catalog snapshot, so scratch would scan fine, but "catalog refresh" and
# "scan -refresh" fetch over HTTPS and need root CA certificates. This
# base is those certificates plus a nonroot passwd entry and nothing else.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/overwater /usr/local/bin/overwater
# os.UserCacheDir needs HOME to place the refreshed catalog; the nonroot
# user owns this directory and nothing else in the image.
ENV HOME=/home/nonroot
WORKDIR /repo
ENTRYPOINT ["/usr/local/bin/overwater"]
CMD ["scan", "/repo"]

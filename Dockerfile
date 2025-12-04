# syntax=docker/dockerfile:1

FROM golang:1.25 AS base
WORKDIR /app

# Development image: used by `make server-run` with hot reload via air.
FROM base AS dev
COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/air-verse/air
# Air v1.61 ignores .air.toml when there are no root-level Go files (defaults to `go build .` and fails).
# Keep config in flags to force the intended build/run command. TODO: drop flags when Air is updated to honor .air.toml here.
COPY .air.toml .
CMD ["air", \
	"-root=.", \
	"-tmp_dir=tmp", \
	"-build.cmd=CGO_ENABLED=0 go build -o ./tmp/server ./cmd/server", \
	"-build.bin=tmp/server", \
	"-build.full_bin=./tmp/server", \
	"-build.include_ext=go,mod,sum", \
	"-build.exclude_dir=tmp,docs,docs_repos,test,data", \
	"-build.exclude_file=**/*_test.go", \
	"-build.pre_cmd=mkdir -p data", \
	"-build.delay=200", \
	"-build.stop_on_error=true"]

# Production build.
FROM base AS builder
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /build/server ./cmd/server

FROM gcr.io/distroless/base-debian12 AS final
WORKDIR /app
COPY --from=builder /build/server /app/server
ENV PORT=5789
ENV STATE_FILE_PATH=/app/data/kkachi_state.json
EXPOSE 5789
VOLUME ["/app/data"]
ENTRYPOINT ["/app/server"]

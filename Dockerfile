# syntax=docker/dockerfile:1

FROM golang:1.25 AS base
WORKDIR /app

# Install ALL required system dependencies
RUN apt-get update && apt-get install -y \
    zsh \
    curl \
    git \
    fontconfig \
    build-essential \
    procps \
    file \
    sudo \
    bison \
    libyaml-dev \
    libreadline-dev \
    libncurses-dev \
    libffi-dev \
    libgdbm-dev \
    libssl-dev \
    zlib1g-dev \
    xz-utils \
    tree \
    neovim \
    && rm -rf /var/lib/apt/lists/*

# Fix ALL dubious ownership issues by trusting everything system-wide
RUN git config --system --add safe.directory '*'

# Setup non-root user for Homebrew
RUN useradd -m -s /bin/zsh linuxbrew && \
    usermod -aG sudo linuxbrew && \
    echo "linuxbrew ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

# Install Homebrew as linuxbrew user
USER linuxbrew
RUN NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"

# Install core tools.
RUN brew install ruby-install chruby eza gnu-tar openjdk@17 python@3.13 libpq
RUN brew link --overwrite python@3.13

# Back to root to install Ruby and Flutter globally
USER root
# Pre-install Ruby 3.3.6
RUN mkdir -p /opt/rubies && ruby-install --install-dir /opt/rubies/ruby-3.3.6 ruby 3.3.6

# Install Flutter via Git (The official way for ARM64 Linux support)
RUN git clone https://github.com/flutter/flutter.git /opt/flutter && \
    cd /opt/flutter && \
    git checkout 3.38.0 && \
    /opt/flutter/bin/flutter precache

# Symlinks for path compatibility
RUN mkdir -p /opt && ln -s /home/linuxbrew/.linuxbrew /opt/homebrew \
    && mkdir -p /Users/draccoon/Develop \
    && ln -s /opt/flutter /Users/draccoon/Develop/flutter \
    && mkdir -p /Users/draccoon/.dart-cli-completion \
    && touch /Users/draccoon/.dart-cli-completion/zsh-config.zsh

# Pre-fetch brew API data
USER linuxbrew
RUN brew update

# Development image
FROM base AS dev
USER root
COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/air-verse/air

COPY .air.toml .
ENV RUBIES=/opt/rubies
ENV HOMEBREW_NO_AUTO_UPDATE=1
ENV HOMEBREW_NO_INSTALL_CLEANUP=1
ENV HOMEBREW_NO_ENV_HINTS=1
ENV HOMEBREW_ALLOW_INSTALL_FROM_API=1

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
USER root
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /build/server ./cmd/server

FROM debian:bookworm-slim AS final
WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    zsh curl git fontconfig build-essential procps file sudo xz-utils tree neovim \
    && rm -rf /var/lib/apt/lists/*

# Copy setups
COPY --from=base /home/linuxbrew /home/linuxbrew
COPY --from=base /opt/rubies /opt/rubies
COPY --from=base /opt/flutter /opt/flutter
COPY --from=base /Users/draccoon /Users/draccoon
COPY --from=base /etc/gitconfig /etc/gitconfig

ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"
RUN mkdir -p /opt && ln -s /home/linuxbrew/.linuxbrew /opt/homebrew

COPY --from=builder /build/server /app/server
ENV PORT=5789
ENV STATE_FILE_PATH=/app/data/kkachi_state.json
ENV SHELL=/bin/zsh
ENV RUBIES=/opt/rubies
ENV HOMEBREW_NO_AUTO_UPDATE=1
ENV HOMEBREW_NO_INSTALL_CLEANUP=1
ENV HOMEBREW_NO_ENV_HINTS=1
ENV HOMEBREW_ALLOW_INSTALL_FROM_API=1

EXPOSE 5789
VOLUME ["/app/data"]
ENTRYPOINT ["/app/server"]

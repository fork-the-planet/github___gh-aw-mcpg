# Dev container for MCP Gateway (Go 1.25)
FROM mcr.microsoft.com/devcontainers/go:1.25-bookworm

# Install golangci-lint
ARG GOLANGCI_LINT_VERSION=v2.8.0
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b /usr/local/bin ${GOLANGCI_LINT_VERSION}

# Install useful CLI tools
RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && apt-get -y install --no-install-recommends \
        jq \
        bash-completion \
    && apt-get autoremove -y && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Pre-download Go modules for faster first build
WORKDIR /tmp/modcache
COPY go.mod go.sum ./
RUN go mod download && rm -rf /tmp/modcache

WORKDIR /workspaces

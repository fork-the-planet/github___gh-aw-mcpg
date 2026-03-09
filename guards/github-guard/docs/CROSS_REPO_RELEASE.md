# Cross-Repository Release Workflow

This document explains the cross-repository release workflow that builds the MCP Gateway from the `gh-aw-mcpg` repository and publishes it to the `github-guard` repository.

## Overview

The workflow `.github/workflows/cross-repo-release.md` is an **agentic workflow** (not a standard GitHub Actions workflow) that:

1. Clones the `https://github.com/github/gh-aw-mcpg` repository
2. Checks out the `lpcox/github-difc` branch
3. Builds the MCP Gateway binary for multiple platforms
4. Runs tests to ensure quality
5. Creates a release in the `lpcox/github-guard` repository
6. Publishes Docker images to `ghcr.io/lpcox/github-guard`
7. Generates SBOMs (Software Bill of Materials)
8. Creates release highlights documentation

## Why Cross-Repository?

The `github-guard` repository contains WASM-based security guards for GitHub MCP servers. The MCP Gateway component is developed in a separate repository (`gh-aw-mcpg`) on a specific branch (`lpcox/github-difc`). This workflow allows us to build and release that component under the `github-guard` organization while maintaining the source in the original repository.

## What is an Agentic Workflow?

This workflow uses the **GitHub Agentic Workflows (gh-aw)** system, which combines YAML frontmatter with markdown instructions for AI agents. Unlike standard GitHub Actions workflows (`.yml` files), agentic workflows:
- Use `.md` (markdown) file extension
- Must be compiled to `.lock.yml` files before they run
- Contain natural language instructions for AI agents
- Support AI-powered automation with tools like bash, edit, and safe-outputs

For more information, see [GitHub Agentic Workflows documentation](../.github/aw/github-agentic-workflows.md).

## How to Trigger

**Important**: Before the workflow can run, it must be compiled to a `.lock.yml` file. This compilation step is handled automatically by the gh-aw system when changes are pushed. If you modify the workflow, the system will regenerate the `.lock.yml` file.

This workflow is triggered manually via workflow dispatch:

1. Go to **Actions** tab in the GitHub repository
2. Select **Cross-Repo Release** workflow
3. Click **Run workflow**
4. Choose the release type:
   - **patch** - Bug fixes (e.g., v1.0.0 → v1.0.1)
   - **minor** - New features (e.g., v1.0.0 → v1.1.0)
   - **major** - Breaking changes (e.g., v1.0.0 → v2.0.0)

## What Gets Published

### 1. Release Artifacts

The following files are uploaded to the GitHub release:

- **Binaries** (pre-built for multiple platforms):
  - `gh-aw-mcpg-linux-amd64`
  - `gh-aw-mcpg-linux-arm64`
  - `gh-aw-mcpg-darwin-amd64` (macOS Intel)
  - `gh-aw-mcpg-darwin-arm64` (macOS Apple Silicon)
  - `gh-aw-mcpg-windows-amd64.exe`
  - `checksums.txt` (SHA256 checksums for verification)

- **SBOMs** (Software Bill of Materials):
  - `sbom.spdx.json` (SPDX format)
  - `sbom.cdx.json` (CycloneDX format)

### 2. Docker Images

Multi-architecture Docker images are published to GitHub Container Registry:

```bash
# Pull the latest version
docker pull ghcr.io/lpcox/github-guard:latest

# Pull a specific version
docker pull ghcr.io/lpcox/github-guard:v1.0.0

# Pull by commit SHA
docker pull ghcr.io/lpcox/github-guard:<commit-sha>
```

**Supported platforms:**
- `linux/amd64`
- `linux/arm64`

### 3. Release Notes

The workflow automatically generates release highlights that include:
- Overview of the cross-repository build
- Key features of MCP Gateway
- Docker image usage instructions
- Links to binaries and documentation
- Reference to the source repository

## Workflow Architecture

### Job 1: clone-and-build

This job handles the entire build and release process:

1. **Clone and checkout**: Clones `gh-aw-mcpg` and checks out `lpcox/github-difc` branch
2. **Version calculation**: Fetches the latest version from `github-guard` releases and increments based on release type
3. **Build and test**: Runs unit tests, builds binary, and runs integration tests
4. **Multi-platform build**: Creates binaries for all supported platforms
5. **Release creation**: Creates a GitHub release in `lpcox/github-guard` with all artifacts
6. **Docker build**: Builds multi-arch Docker image and pushes to `ghcr.io/lpcox/github-guard`
7. **SBOM generation**: Creates software bill of materials and attaches to release

### Job 2: generate-highlights

This job generates and prepends release highlights:

1. **Fetch release data**: Gets information about the current and previous releases
2. **Clone source**: Clones the source repository for context (README, docs)
3. **AI generation**: Uses the agentic workflow AI to generate appropriate release notes
4. **Update release**: Prepends the generated highlights to the release body

## Technical Details

### Version Management

The workflow determines the next version by:
1. Querying the GitHub API for the latest release in `lpcox/github-guard`
2. If no releases exist, starts from `v0.0.0`
3. Increments based on the selected release type (major/minor/patch)
4. Creates a new tag with the calculated version

### Build Process

The build follows the standard Go build process from the `gh-aw-mcpg` repository:
1. Downloads Go modules
2. Runs `make test-unit` (unit tests - failures logged but not blocking)
3. Runs `make build` (binary compilation - **must succeed or workflow fails**)
4. Runs `make test-integration` (integration tests - failures logged but not blocking)
5. Builds release binaries for all platforms with version metadata

**Note**: Tests are run for quality assurance but do not block the release. This allows the workflow to proceed even if the source repository has test issues. However, **build failures will stop the workflow** as they indicate the code cannot compile. Review test output in the workflow logs to ensure quality.

### Docker Image

The Docker image is built using:
- **Docker Buildx**: For multi-architecture builds
- **QEMU**: For cross-platform emulation
- **GitHub Actions Cache**: For faster subsequent builds
- **Version build arg**: Embeds version information in the binary

### Security Features

- **SBOM Generation**: Creates software bill of materials for supply chain security using Syft
- **Multi-arch Support**: Ensures consistent behavior across architectures
- **Checksum Verification**: Provides SHA256 checksums for all binaries
- **Network Sandboxing**: Workflow runs in a sandboxed environment with restricted network access

**Note**: This workflow does not perform active secret scanning beyond basic SBOM auditing. Ensure the source repository has proper security scanning in place.

## Prerequisites

### Required Permissions

The workflow requires the following GitHub token permissions:
- `contents: write` - Create releases and upload artifacts
- `packages: write` - Push Docker images to GHCR
- `pull-requests: read` - Read PR data for release notes
- `actions: read` - Access workflow context
- `issues: read` - Access issue data
- `id-token: write` - Attestations support
- `attestations: write` - Create attestations

### Required Roles

Only users with the following roles can trigger the workflow:
- `admin`
- `maintainer`

### Network Access

The workflow requires network access to:
- GitHub APIs (default)
- Node package registries
- `ghcr.io` (GitHub Container Registry)
- `github.com` (for cloning)
- `raw.githubusercontent.com` (for fetching files)

## Troubleshooting

### Build Failures

If the build fails, check:
1. The `lpcox/github-difc` branch exists in the `gh-aw-mcpg` repository
2. The branch has valid Go code that compiles
3. Tests pass locally in the source repository
4. The `Makefile` has the expected targets (`test-unit`, `build`, `test-integration`)

### Release Creation Failures

If release creation fails, check:
1. The calculated version doesn't already exist
2. The GitHub token has `contents: write` permission
3. The repository allows releases to be created

### Docker Push Failures

If Docker image push fails, check:
1. GitHub token has `packages: write` permission
2. GHCR is enabled for the repository
3. The actor has permission to push to the package

## Maintenance

### Updating the Workflow

To modify the workflow:
1. Edit `.github/workflows/cross-repo-release.md`
2. Test changes by running the workflow manually
3. Commit changes to the repository

### Updating Source Branch

If the source branch in `gh-aw-mcpg` changes:
1. Update all references to `lpcox/github-difc` in the workflow
2. Update this documentation

### Updating Docker Registry

If the Docker registry location changes:
1. Update all references to `ghcr.io/lpcox/github-guard` in the workflow
2. Update the login action if using a different registry
3. Update this documentation

## Related Documentation

- [GitHub Guard Main README](../README.md)
- [Quick Start Guide](./QUICKSTART.md)
- [Testing Guide](./TESTING.md)
- [Source Repository (gh-aw-mcpg)](https://github.com/github/gh-aw-mcpg)
- [GitHub Agentic Workflows](../.github/aw/github-agentic-workflows.md)

## Support

For issues or questions:
1. Check the [GitHub Guard Issues](https://github.com/lpcox/github-guard/issues)
2. Review workflow run logs in the Actions tab
3. Consult the source repository documentation

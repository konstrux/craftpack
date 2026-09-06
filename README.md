<!--
SPDX-FileCopyrightText: 2026 Marcin Kaim
SPDX-License-Identifier: Apache-2.0
-->

# Craftpack

**Standardized Linux Packaging Factory for the Software Delivery Platform (SDP)**

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![REUSE 3.3 Compliant](https://img.shields.io/badge/REUSE-compliant-green.svg)](https://reuse.software/)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://golang.org)
[![Packaging Target](https://img.shields.io/badge/Target-Debian%20(.deb)-D70A53.svg)](https://www.debian.org)

---

## 1. Overview

**Craftpack** functions as the automated packaging factory (**Component 4**) within the Software Delivery Platform (SDP). It takes pre-compiled application payloads, documentation sources, configuration templates, and declarative specification manifests (`craftpack.yml`), and compiles them into immutable, production-grade Linux distribution packages (e.g., Debian `.deb` containers).

Operating entirely in pure Go (`CGO_ENABLED=0`), Craftpack eliminates all dependencies on host packaging toolchains such as `dpkg-deb`, `ar`, `tar`, or `gzip`. It provides predictable, hermetic, bit-for-bit reproducible packaging that runs seamlessly in constrained CI/CD runners, scratch containers, and air-gapped deployment environments.

---

## 2. Key Architecture Pillars

* **Zero External Runtime Dependencies**: Implemented strictly in pure Go without CGO or external shell utilities. The engine contains built-in native implementations for Unix `ar` container assembly, tar layer construction, gzip compression, cryptographic hashing, and roff manual page rendering.
* **FHS Payload Isolation**: Private binaries, vendored libraries, and shared assets are installed exclusively into `/usr/lib/<name>/`, ensuring complete namespace isolation and avoiding collisions across packages.
* **Transparent Proxy Launcher**: Synthesizes a lightweight, POSIX-compliant `/bin/sh` launcher installed to `/usr/bin/<command>`. Using atomic POSIX `exec "$REAL_PAYLOAD" "$@"` semantics, the launcher replaces its process image without subshell overhead, preserving standard process tree topologies, signals (`SIGINT`, `SIGTERM`), and stream channels.
* **Decoupled Manual Page Synthesis (Zero-Markup)**: Compiles source Markdown documentation into compressed roff manual pages installed into `/usr/share/man/man[1-8]/`. Under the Zero-Markup policy, Markdown files remain standard and readable without proprietary YAML front-matter delimiters (`---`).
* **Global Configuration Management**: Default configuration templates are deployed to `/etc/<name>/` and automatically registered in `DEBIAN/conffiles` to guarantee user modifications are never overwritten during package upgrades.
* **Bit-for-Bit Deterministic Reproducibility**: Tar header metadata is sorted alphabetically, file ownership is mapped to `root:root` (UID/GID 0), permissions are normalized (`0755` for directories/executables, `0644` for regular files), and file timestamps honor the standard `SOURCE_DATE_EPOCH` environment variable.
* **Strict Stream Separation**: Clean, machine-parseable data payloads (such as JSON validation reports or version telemetry) are emitted strictly to `STDOUT`. Diagnostic logging, status indicators, and error traces are routed exclusively to `STDERR`.
* **Autonomous Self-Packaging ($N \to N$)**: Craftpack packages its own distribution artifacts using its freshly compiled binary, avoiding bootstrap cycles or network dependencies on previous releases.

---

## 3. Packaging Pipeline Architecture

The packaging engine executes a deterministic 7-stage build lifecycle:

```
                  +-------------------------------------------------------+
                  |  Stage 1: CLI Ingestion & Schema Validation           |
                  |  - Parse flags, validate craftpack.yml schema         |
                  |  - Assert workspace boundaries & traversal safety     |
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 2: Staging Area Setup & Payload Crawling       |
                  |  - Allocate ephemeral staging workspace               |
                  |  - Crawl payload_dir, validate regular files          |
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 3: Proxy Launcher Synthesis                    |
                  |  - Generate POSIX /bin/sh launcher at /usr/bin/<cmd>  |
                  |  - Anchor REAL_PAYLOAD to /usr/lib/<name>/<entrypoint>|
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 4: Documentation Staging                       |
                  |  - Validate Zero-Markup (no front-matter)             |
                  |  - Compile Markdown to roff via md2man                |
                  |  - Compress with gzip into /usr/share/man/man[1-8]/   |
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 5: Target Metadata Synthesis                   |
                  |  - Generate DEBIAN/control, conffiles, md5sums        |
                  |  - Validate maintainer scripts (pre/postinst, prerm)  |
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 6: Archive Compilation                         |
                  |  - Assemble control.tar.gz and data.tar.gz            |
                  |  - Compile pure-Go Unix ar archive (.deb container)   |
                  +---------------------------+---------------------------+
                                              |
                                              v
                  +-------------------------------------------------------+
                  |  Stage 7: Release Manifest & Staging Cleanup          |
                  |  - Compute package SHA-256 digest & file size         |
                  |  - Write checksums.sha256 manifest                    |
                  |  - Atomically purge ephemeral staging workspace       |
                  +-------------------------------------------------------+
```

---

## 4. Installation & Requirements

### Prerequisites
* **Operating System**: Linux (x86_64, ARM64)
* **Runtime**: Zero dependencies (standalone statically linked binary)
* **Build Toolchain** (if compiling from source): Go 1.22 or higher

### Installing via Pre-Compiled Debian Package
```bash
# Install package using dpkg
sudo dpkg -i craftpack_1.0.0_amd64.deb

# Verify installation
craftpack --version-info
```

### Compiling from Source
```bash
# Clone the repository
git clone https://github.com/craftpack/craftpack.git
cd craftpack

# Compile static binary
CGO_ENABLED=0 go build -ldflags "-s -w" -o craftpack ./cmd/craftpack

# Verify binary execution
./craftpack --help
```

---

## 5. CLI Usage & Commands

Craftpack provides two primary operational subcommands: `build` and `validate`.

### 5.1. `craftpack build`

Compiles and packages the application into an immutable distribution container.

```bash
craftpack build --spec PATH --target deb --package-version VERSION [OPTIONS]
```

#### Flags & Options:
| Flag | Short | Type | Default | Description |
| :--- | :---: | :---: | :---: | :--- |
| `--spec` | `-s` | `string` | `craftpack.yml` | Path to specification manifest. |
| `--target` | `-t` | `string` | *(mandatory)* | Packaging target format (`deb`). |
| `--package-version` | `-p` | `string` | *(mandatory)* | SemVer 2.0.0 version string (e.g. `1.0.0`, `v2.1.0`). |
| `--output-dir` | `-o` | `string` | `./dist` | Output directory for `.deb` and `checksums.sha256`. |
| `--arch` | `-a` | `string` | *(host arch)* | Target CPU architecture override (`amd64`, `arm64`, `all`). |
| `--dry-run` | `-d` | `bool` | `false` | Simulate all 7 stages without writing package to disk. |
| `--strict` | | `bool` | `false` | Elevate unknown schema fields from warnings to errors. |
| `-v`, `--verbose` | | `bool` | `false` | Enable verbose / debug logging on `STDERR`. |
| `-q`, `--quiet` | | `bool` | `false` | Suppress non-error logging output on `STDERR`. |
| `--log-level` | | `string` | `info` | Explicit log level: `trace`, `debug`, `info`, `warn`, `error`. |

#### Examples:
```bash
# Standard Debian package build
craftpack build --target deb --package-version 1.0.0

# Dry-run build simulation with verbose output
craftpack build --target deb --package-version 2.0.0-rc.1 --dry-run -v

# Cross-architecture build (ARM64) to custom output folder
craftpack build --spec my-app.yml --target deb --package-version 1.2.0 --arch arm64 --output-dir ./release

# Bit-for-bit reproducible packaging using SOURCE_DATE_EPOCH
SOURCE_DATE_EPOCH=1700000000 craftpack build --target deb --package-version 1.0.0
```

---

### 5.2. `craftpack validate`

Performs static validation of the `craftpack.yml` manifest and asserts workspace preconditions without compiling artifacts.

```bash
craftpack validate [--spec PATH] [--strict] [--json]
```

#### Flags & Options:
| Flag | Short | Type | Default | Description |
| :--- | :---: | :---: | :---: | :--- |
| `--spec` | `-s` | `string` | `craftpack.yml` | Path to specification manifest. |
| `--strict` | | `bool` | `false` | Fail on unrecognized YAML keys or target blocks. |
| `--json` | | `bool` | `false` | Emit structured JSON validation summary on `STDOUT`. |

#### Examples:
```bash
# Validate local craftpack.yml
craftpack validate

# Strict validation with JSON output for automated CI pipelines
craftpack validate --strict --json
```

---

### 5.3. Version and Telemetry Information

```bash
# Concise version output
craftpack --version

# Comprehensive build and runtime telemetry
craftpack --version-info

# Machine-readable telemetry in JSON format
craftpack --version-info --json
```

---

### 5.4. Exit Status Codes

Craftpack emits deterministic process exit status codes:

| Code | Symbol | Description |
| :---: | :--- | :--- |
| `0` | `ExitSuccess` | Command completed successfully. |
| `1` | `ExitValidation` | Specification error, missing file, destructive hook, or boundary violation. |
| `2` | `ExitUsage` | Invalid CLI syntax, missing mandatory flags, or unrecognized options. |
| `130` | `ExitTerminated` | Process execution interrupted by OS signal (`SIGINT`, `SIGTERM`). |

---

## 6. Specification Guide (`craftpack.yml`)

The declarative `craftpack.yml` file defines package identity, file layouts, documentation, and target platform settings.

### Schema Reference

| Field | Type | Required | Description & Constraints |
| :--- | :---: | :---: | :--- |
| `name` | `string` | **Yes** | Package identity. Lowercase alphanumeric and single hyphens (1–64 chars). |
| `description` | `string` | **Yes** | Single-line synopsis (10–150 chars, plain text, no markdown/shell syntax). |
| `maintainer` | `string` | **Yes** | RFC 822 format: `Name <email@example.com>`. |
| `homepage` | `string` | **Yes** | Valid `http://` or `https://` project URL. |
| `license` | `string` | **Yes** | Valid SPDX license identifier (e.g. `Apache-2.0`, `MIT`). |
| `command` | `string` | **Yes** | Public command name installed into `/usr/bin/<command>`. |
| `payload_dir` | `string` | **Yes** | Relative workspace directory containing pre-compiled files. |
| `entrypoint` | `string` | **Yes** | Relative path to executable within `payload_dir`. |
| `man_pages` | `list` | No | Markdown files to compile into manual pages. |
| `man_pages[].source` | `string` | **Yes** | Relative path to Markdown file (Zero-Markup policy applies). |
| `man_pages[].section` | `int` | **Yes** | Unix manual section (`1` to `8`). Default: `1`. |
| `man_pages[].title` | `string` | No | Man page title in uppercase (defaults to uppercase `command`). |
| `man_pages[].header` | `string` | No | Category header (defaults to standard UNIX section title). |
| `man_pages[].footer` | `string` | No | Footer label (defaults to `<name> <version>`). |
| `default_config` | `map` | No | Mapping of workspace config sources to `/etc/<command>/<target>`. |
| `preinstall` | `string` | No | Hook script path or inline script executed before installation. |
| `postinstall` | `string` | No | Hook script path or inline script executed after installation. |
| `preremove` | `string` | No | Hook script path or inline script executed before package removal. |
| `postremove` | `string` | No | Hook script path or inline script executed after package removal. |
| `targets.deb.section` | `string` | No | Debian archive category (e.g. `utils`, `devel`). Default: `utils`. |
| `targets.deb.priority` | `string` | No | Debian package priority (`optional`, `standard`). Default: `optional`. |
| `targets.deb.dependencies` | `list` | No | Runtime dependencies with optional versions (e.g. `libc6 (>= 2.31)`). |

### Complete `craftpack.yml` Example

```yaml
# SPDX-FileCopyrightText: 2026 Marcin Kaim
# SPDX-License-Identifier: Apache-2.0

name: craftpack
description: Standardized Linux packaging factory for the Software Delivery Platform
maintainer: Marcin Kaim <9829098+marcinkaim@users.noreply.github.com>
homepage: https://github.com/marcinkaim/craftpack
license: Apache-2.0

command: craftpack
payload_dir: dist/payload
entrypoint: bin/craftpack

man_pages:
  - source: docs/manual.md
    section: 1
    title: CRAFTPACK
    header: User Commands Manual
    footer: Craftpack Packaging Utility

default_config:
  config/craftpack.default.yml: craftpack.yml

targets:
  deb:
    section: utils
    priority: optional
    dependencies:
      - libc6 (>= 2.31)
```

---

## 7. Autonomous Self-Packaging ($N \to N$) & CI/CD

Craftpack uses the autonomous **$N \to N$ self-packaging model**:
1. Version $N$ of the executable is compiled from source.
2. Binary $N$ directly invokes its own `build` command on `craftpack.yml`.
3. Binary $N$ stages itself into `/usr/lib/craftpack/bin/craftpack`, synthesizes the `/usr/bin/craftpack` proxy launcher, compiles `docs/manual.md` into `/usr/share/man/man1/craftpack.1.gz`, deploys `/etc/craftpack/craftpack.yml`, and seals the `.deb` container.
4. Dogfooding is inherently achieved: a broken binary will fail to self-package.

### GitHub Actions Release Pipeline (`.github/workflows/release.yml`)

```yaml
name: Release Craftpack

on:
  push:
    tags:
      - "v*"

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      # 1. Compile and unit-test version N
      - name: Build & Test Binary (N)
        run: |
          go test ./...
          mkdir -p dist/payload/bin
          go build -ldflags "-s -w -X main.version=${{ github.ref_name }}" -o dist/payload/bin/craftpack ./cmd/craftpack

      # 2. Self-Package: version N packages itself
      - name: Self-Package
        run: |
          ./dist/payload/bin/craftpack build \
            --spec craftpack.yml \
            --package-version "${{ github.ref_name }}" \
            --target deb \
            --output-dir ./dist

      # 3. Publish release assets
      - name: Publish Release
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          gh release create "${{ github.ref_name }}" dist/*.deb dist/checksums.sha256 \
            --title "Release ${{ github.ref_name }}" \
            --generate-notes
```

---

## 8. Development & Testing

Craftpack is tested using Go's built-in testing framework with high test coverage across unit and integration levels.

```bash
# Run all unit and integration tests
go test -v -count=1 ./...

# Run static code analysis
go vet ./...

# Check REUSE 3.3 licensing compliance
reuse lint
```

---

## 9. License & Legal Compliance

Craftpack is licensed under the **Apache License, Version 2.0**.

This project complies strictly with version 3.3 of the [REUSE Specification](https://reuse.software/). Every source file, configuration template, and documentation document contains explicit SPDX copyright and license identifiers.

* **SPDX-FileCopyrightText**: 2026 Marcin Kaim
* **SPDX-License-Identifier**: Apache-2.0
* **Author & Maintainer**: Marcin Kaim <9829098+marcinkaim@users.noreply.github.com>

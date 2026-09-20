<!--
SPDX-FileCopyrightText: 2026 Marcin Kaim
SPDX-License-Identifier: GPL-3.0-only
-->

# Craftpack

**Standardized Linux packaging factory for the Software Delivery Platform (SDP) that builds production-grade Debian (.deb) packages from declarative specifications.**

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![REUSE 3.3 Compliant](https://img.shields.io/badge/REUSE-compliant-green.svg)](https://reuse.software/)
[![Attestation](https://img.shields.io/badge/Attestation-GitHub_Artifact_Attestations-blueviolet.svg)](https://github.com/actions/attest-build-provenance)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://golang.org)
[![Packaging Target](https://img.shields.io/badge/Target-Debian%20(.deb)-D70A53.svg)](https://www.debian.org)

---

## 1. Overview

**Craftpack** automates the assembly of compiled software payloads, documentation sources, and configuration templates into system-compliant Linux packages (`.deb`). It operates as a self-contained packaging factory that compiles archives directly from declarative `craftpack.yml` manifests.

Craftpack is implemented in pure Go (`CGO_ENABLED=0`) and has zero dependencies on host packaging utilities such as `dpkg-deb`, `ar`, `tar`, or `gzip`. It produces hermetic, bit-for-bit reproducible packages across container runners, CI/CD pipelines, and air-gapped environments.

---

## 2. Features

- **Zero External Runtime Dependencies:** Written in pure Go with built-in Unix `ar` archive assembly, tar construction, gzip compression, MD5/SHA-256 calculation, and roff manual page rendering.
- **Direct Binary Placement:** Installs standalone compiled binaries directly into `/usr/bin/` by default (`wrapper: false`), avoiding unnecessary launcher scripts.
- **Isolated Proxy Launcher:** Optionally stages application assets in private vaults (`/usr/lib/<name>/`) with a POSIX `/bin/sh` proxy launcher (`wrapper: true`) using atomic `exec` process replacement.
- **Zero-Markup Manual Page Generation:** Converts standard Markdown documentation into compressed roff manual pages (`/usr/share/man/man[1-8]/`) without requiring YAML front-matter delimiters.
- **Deterministic & Reproducible Builds:** Standardized file permissions (`0755` for executables/directories, `0644` for files), normalized ownership (`root:root`), alphabetical tar header sorting, and full support for the `SOURCE_DATE_EPOCH` environment variable.
- **Strict Stream Separation:** Machine-parseable payloads (JSON reports, version info) are emitted strictly to `STDOUT`. All diagnostic logs, progress notices, and errors are routed to `STDERR`.
- **Autonomous Self-Packaging ($N \to N$):** Craftpack uses its freshly compiled executable to package its own distribution `.deb` package without circular toolchain dependencies.

---

## 3. Prerequisites

* **Operating System:** Linux (x86_64, ARM64)
* **Runtime:** Zero runtime dependencies (statically linked binary)
* **Build Toolchain** *(only if compiling from source)*: Go 1.22 or higher

---

## 4. Installation

### Debian Package (.deb)

Download and install the pre-compiled `.deb` package from the [Releases](https://github.com/konstrux/craftpack/releases) page:

```bash
# Download package for your architecture (amd64 or arm64)
curl -sSLO https://github.com/konstrux/craftpack/releases/latest/download/craftpack_1.0.0_amd64.deb

# Install using dpkg
sudo dpkg -i craftpack_1.0.0_amd64.deb

# Verify installation
craftpack --version-info
```

> [!NOTE]
> Official release packages include cryptographically signed build provenance generated via **GitHub Artifact Attestations** (SLSA v1.0 / Sigstore). You can verify package provenance using the GitHub CLI:
> ```bash
> gh attestation verify craftpack_1.0.0_amd64.deb --repo konstrux/craftpack
> ```

### Build from Source

```bash
# Clone the repository
git clone https://github.com/konstrux/craftpack.git
cd craftpack

# Compile static binary
CGO_ENABLED=0 go build -ldflags "-s -w" -o craftpack ./cmd/craftpack

# Verify execution
./craftpack --help
```

---

## 5. Quick Start

### 1. Initialize a Specification File

```bash
# Scaffold standard Debian specification
craftpack init

# List available packaging templates
craftpack init --list
```

### 2. Validate a Specification File

```bash
craftpack validate --spec craftpack.yml --strict
```

### 3. Build a Package

```bash
craftpack build --spec craftpack.yml --target deb --package-version 1.0.0 --output-dir ./dist
```

### Common Flags

| Flag | Short | Type | Default | Description |
| :--- | :---: | :---: | :---: | :--- |
| `--spec` | `-s` | `string` | `craftpack.yml` | Path to specification manifest. |
| `--target` | `-t` | `string` | `deb` | Target distribution format (`deb`). |
| `--package-version` | | `string` | *(required)* | Package release version (SemVer 2.0.0). |
| `--output-dir` | `-o` | `string` | `dist` | Output directory for built package and checksums. |
| `--dry-run` | | `bool` | `false` | Run packaging stages without writing output files. |
| `--strict` | | `bool` | `false` | Fail build if specification produces any warnings. |
| `-v, --verbose` | `-v` | `count` | `0` | Increase log verbosity (`-v`: DEBUG, `-vv`: TRACE). |
| `-q, --quiet` | `-q` | `bool` | `false` | Suppress diagnostic output, showing only errors. |
| `--json` | | `bool` | `false` | Output results in machine-readable JSON format. |

For the complete command reference and advanced flag options, refer to the user manual at [docs/manuals/craftpack.1.md](docs/manuals/craftpack.1.md) and the configuration specification manual at [docs/manuals/craftpack.yml.5.md](docs/manuals/craftpack.yml.5.md).

---

## 6. Configuration

Craftpack is configured via a declarative YAML file (`craftpack.yml`). Below is a minimal example:

```yaml
name: my-app
description: High-performance backend service for the SDP
maintainer: Developer <dev@example.com>
homepage: https://example.com/my-app
license: Apache-2.0

command: my-app
payload_dir: dist/payload
entrypoint: bin/my-app
targets:
  deb:
    section: utils
    priority: optional
    wrapper: false
    dependencies:
      - libc6 (>= 2.31)
```

> [!TIP]
> For the complete specification schema, advanced features (manual pages, default configuration files, and maintainer hooks), refer to:
> - [docs/specification.md](docs/specification.md) - Full specification and schema reference
> - [craftpack.yml](craftpack.yml) - The project's own packaging manifest
> - [templates/deb.yml](templates/deb.yml) - Documented packaging specification template for Debian targets

---

## 7. Packaging Pipeline Architecture

Craftpack executes a deterministic 7-stage build lifecycle:

```text
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
|  - Stage direct binary (/usr/bin) or vault (/usr/lib) |
+---------------------------+---------------------------+
                            |
                            v
+-------------------------------------------------------+
|  Stage 3: Proxy Launcher Synthesis (Conditional)      |
|  - Generate POSIX /bin/sh launcher when wrapper=true  |
|  - Bypass launcher synthesis when wrapper=false       |
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

Release builds automate this pipeline in GitHub Actions using the autonomous self-packaging workflow defined in [.github/workflows/release.yml](.github/workflows/release.yml).

---

## 8. Development

You are free to fork, modify, and experiment with the codebase under the terms of the project license.

### Running Tests

Execute unit and integration tests:

```bash
# Run unit and integration tests
go test -v -count=1 ./...

# Run race detector tests
go test -race ./...
```

### Code Quality and Compliance

```bash
# Run static code analysis
go vet ./...

# Verify REUSE 3.3 licensing compliance
reuse lint
```

---

## 9. License

Craftpack is licensed under the **GNU General Public License, Version 3** (GPLv3).

* **SPDX-FileCopyrightText:** 2026 Marcin Kaim
* **SPDX-License-Identifier:** `GPL-3.0-only`
* **License File:** [LICENSE](LICENSE)
* **Full Text:** [LICENSES/GPL-3.0-only.txt](LICENSES/GPL-3.0-only.txt)

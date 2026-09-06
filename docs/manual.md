<!--
SPDX-FileCopyrightText: 2026 Marcin Kaim
SPDX-License-Identifier: Apache-2.0
-->

# craftpack

## NAME

craftpack - Standardized Linux packaging factory for the Software Delivery Platform

## SYNOPSIS

**craftpack** [*GLOBAL OPTIONS*] <*COMMAND*> [*OPTIONS*]

**craftpack build** [**--spec** *PATH*] [**--target** *TARGET*] [**--package-version** *VERSION*] [**--output-dir** *DIR*] [**--arch** *ARCH*] [**--dry-run**] [**--strict**] [**--json**]

**craftpack validate** [**--spec** *PATH*] [**--strict**] [**--json**]

**craftpack** [**-V** | **--version** | **--version-info**] [**--json**]

**craftpack** [**-h** | **--help**]

## DESCRIPTION

**Craftpack** serves as the automated packaging factory (Component 4) within the Software Delivery Platform (SDP). It ingests pre-compiled application payloads, documentation, configuration templates, and declarative specification manifests (`craftpack.yml`), compiling them into immutable, system-compliant Linux distribution packages.

Designed around modern security, isolation, and portability principles, Craftpack operates entirely in pure Go (`CGO_ENABLED=0`) with zero external runtime dependencies—eliminating any reliance on host packaging tools such as **dpkg-deb**, **ar**, **tar**, or **gzip**.

### Core Architecture and Mechanics

* **Payload Isolation and FHS Compliance**: Application executables, internal libraries, and private assets are deployed exclusively into `/usr/lib/<app_id>/`, preserving nested hierarchies and preventing namespace pollution.
* **Transparent Proxy Launcher**: Craftpack synthesizes a lightweight, POSIX-compliant `/bin/sh` wrapper installed to `/usr/bin/<command>`. The launcher uses native POSIX `exec` semantics to replace its process image with the private payload binary, guaranteeing transparent signal handling, unbuffered stream passthrough, and elimination of shell injection risks.
* **Decoupled Manual Page Synthesis**: Markdown documentation files declared in the specification are compiled on-the-fly into roff manual formatting and compressed with gzip. Under the Zero-Markup policy, source Markdown remains clean and free of platform-specific headers or front-matter.
* **Global Configuration Deployment**: Default configuration templates are staged in `/etc/<app_id>/` and automatically registered in package control indices (such as `DEBIAN/conffiles`) to safeguard user configurations during upgrades.
* **Deterministic and Reproducible Builds**: Tar archive entries are sorted alphabetically, file modes are normalized (`0755` for directories/executables, `0644` for regular files), ownership is assigned to `root:root` (UID/GID 0), and timestamps honor the standard `SOURCE_DATE_EPOCH` environment variable.
* **Strict Stream Separation**: Clean, machine-readable data payloads (such as JSON summaries or version information) are emitted strictly to STDOUT. All diagnostic logs, progress notifications, warnings, and errors are routed to STDERR.
* **Autonomous Self-Packaging ($N \to N$)**: Craftpack packages itself using its own freshly compiled executable, eliminating external network bootstrap dependencies and ensuring offline assembly compliance.

## COMMANDS

* **build**
  Compiles and bundles the target application into an immutable distribution package according to the specification manifest. The build process executes a strict 7-stage lifecycle pipeline:
  1. *Stage 1: CLI Ingestion & Schema Validation* - Parses input flags, evaluates workspace boundaries, and validates schema constraints.
  2. *Stage 2: Staging Area Setup & Payload Crawling* - Allocates an ephemeral workspace, crawls `payload_dir`, and validates regular files.
  3. *Stage 3: Proxy Launcher Synthesis* - Generates the `/usr/bin/<command>` proxy script using POSIX `exec` delegation.
  4. *Stage 4: Documentation Staging* - Compiles Markdown sources into compressed roff man pages under `/usr/share/man/man[1-8]/`.
  5. *Stage 5: Target Metadata Synthesis* - Generates target control files (`DEBIAN/control`, `conffiles`, maintainer hooks, and `md5sums`).
  6. *Stage 6: Archive Compilation* - Sequentially assembles archive layers into the target container (e.g. Unix `ar` container for `.deb`).
  7. *Stage 7: Release Manifest Generation & Staging Cleanup* - Computes SHA-256 digests, records `checksums.sha256`, and removes temporary staging assets.

* **validate**
  Parses and validates the project specification manifest (`craftpack.yml`) and asserts local workspace prerequisites without compiling or assembling distribution containers. Verifies package identity, SemVer versioning, maintainer syntax (RFC 822), homepage URI schemes, SPDX license identifiers, command collision blacklists, and workspace traversal boundaries.

## OPTIONS

### Global Options

* **-v**, **--verbose**
  Increase logging verbosity to debug level. May be combined with other log flags; flag precedence resolves using Last-Flag-Wins.

* **-q**, **--quiet**
  Suppress all informational and debug logging, outputting only actionable errors to STDERR.

* **--log-level** *LEVEL*
  Explicitly specify the minimum logging threshold. Recognized values: `trace`, `debug`, `info`, `warn`, `error`. Default: `info`.

* **-V**, **--version**
  Print the concise application version string to STDOUT and exit successfully.

* **--version-info**
  Print comprehensive build and runtime telemetry (Git commit, build date, Go version, architecture, platform) to STDOUT and exit successfully.

* **--json**
  Format command output as structured JSON on STDOUT. When combined with `--version-info`, outputs build telemetry JSON.

* **-h**, **--help**
  Display contextual help and command usage syntax.

### Build Subcommand Options (`craftpack build`)

* **-s**, **--spec** *PATH*
  Filesystem path to the packaging specification manifest. Defaults to `craftpack.yml` within the active working directory.

* **-t**, **--target** *TARGET*
  Target packaging format. Currently supported: `deb` (Debian archive). This flag is mandatory during build execution.

* **-p**, **--package-version** *VERSION*
  Semantic version string conforming strictly to SemVer 2.0.0 (e.g. `1.0.0`, `v2.1.0`, `0.9.0-rc.1+build.12`). Leading `v`/`V` prefixes are automatically normalized. This flag is mandatory during build execution.

* **-o**, **--output-dir** *DIR*
  Destination directory where compiled packages and `checksums.sha256` release manifests are written. Defaults to `./dist`. Parent directories are created automatically if nonexistent.

* **-a**, **--arch** *ARCH*
  Target CPU architecture override (e.g. `amd64`, `arm64`, `all`, `x86_64`, `aarch64`). If omitted, defaults to the detected host CPU architecture.

* **-d**, **--dry-run**
  Simulate all build stages (validation, staging, launcher synthesis, metadata compilation) without creating container archives or release manifests on disk.

* **--strict**
  Enable strict schema validation. In default mode, unrecognized YAML configuration keys emit non-blocking warnings (forward tolerance); in strict mode, unknown keys trigger immediate validation errors.

### Validate Subcommand Options (`craftpack validate`)

* **-s**, **--spec** *PATH*
  Filesystem path to the specification file to validate. Defaults to `craftpack.yml` within the working directory.

* **--strict**
  Enable strict validation mode. Elevates forward-tolerance warnings for unknown keys or target blocks into fail-fast validation errors.

* **--json**
  Output structured validation results to STDOUT, including validation status, package name, specification path, and warning messages.

## ENVIRONMENT VARIABLES

* **CRAFTPACK_LOG_LEVEL**
  Specifies the default logging level if not overridden by command-line flags. Supported values: `trace`, `debug`, `info`, `warn`, `error`.

* **NO_COLOR**
  When set to any non-empty value, disables ANSI terminal escape codes and colorized logging output on STDERR.

* **SOURCE_DATE_EPOCH**
  Unix epoch timestamp (in seconds) used to set deterministic file modification times in archive headers, enabling bit-for-bit reproducible packaging builds.

## FILES

* **craftpack.yml**
  The declarative YAML specification describing application identity, payload boundaries, entrypoint, documentation, configurations, lifecycle hooks, and target-specific parameters.

* **/etc/<app_id>/<config>**
  System configuration directory where default application configuration templates are installed.

* **/usr/bin/<command>**
  Public proxy launcher wrapper script installed into the host system `PATH`.

* **/usr/lib/<app_id>/**
  Isolated directory containing the private executable binary, libraries, and vendored assets.

* **/usr/share/man/man[1-8]/<command>.[1-8].gz**
  Compiled and gzipped Unix manual pages generated from source Markdown.

* **checksums.sha256**
  Cryptographic release manifest generated in the output directory recording SHA-256 digests and file sizes.

## EXIT STATUS

* **0**
  Success (`ExitSuccess`). Command completed execution successfully.

* **1**
  Validation or Boundary Error (`ExitValidation`). Triggered when configuration validation fails, required files are missing, destructive commands are detected in hooks, or workspace traversal violations occur.

* **2**
  CLI Usage or Syntax Error (`ExitUsage`). Triggered by invalid command syntax, unrecognized flags, missing mandatory flags, or extraneous arguments.

* **130**
  Execution Terminated (`ExitTerminated`). Process execution was interrupted by an operating system signal (such as `SIGINT` or `SIGTERM`).

## EXAMPLES

* Validate a packaging specification in the current directory:
  ```bash
  craftpack validate
  ```

* Validate a specification in strict mode with JSON output:
  ```bash
  craftpack validate --spec config/craftpack.yml --strict --json
  ```

* Build a standard Debian package for release:
  ```bash
  craftpack build --target deb --package-version 1.0.0
  ```

* Simulate a dry-run build with debug logging enabled:
  ```bash
  craftpack build --target deb --package-version 2.0.0-rc.1 --dry-run -v
  ```

* Build a package for ARM64 architecture with a custom output directory:
  ```bash
  craftpack build --spec craftpack.yml --target deb --package-version 1.2.0 --arch arm64 --output-dir ./release
  ```

* Generate bit-for-bit reproducible package using `SOURCE_DATE_EPOCH`:
  ```bash
  SOURCE_DATE_EPOCH=1700000000 craftpack build --target deb --package-version 1.0.0
  ```

* Self-package Craftpack using its own compiled binary:
  ```bash
  go build -o dist/payload/bin/craftpack ./cmd/craftpack
  ./dist/payload/bin/craftpack build --package-version 1.0.0 --target deb --output-dir ./dist
  ```

## AUTHORS

Written and maintained by **Marcin Kaim** <9829098+marcinkaim@users.noreply.github.com>.

## SEE ALSO

**dpkg(1)**, **deb(5)**, **ar(1)**, **gzip(1)**

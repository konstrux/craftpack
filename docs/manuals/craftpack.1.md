<!--
SPDX-FileCopyrightText: 2026 Marcin Kaim
SPDX-License-Identifier: GPL-3.0-only
-->

# craftpack

## NAME

craftpack - Standardized Linux packaging factory for the Software Delivery Platform

## SYNOPSIS

**craftpack** [*GLOBAL OPTIONS*] <*COMMAND*> [*OPTIONS*]

**craftpack init** [*TEMPLATE*] [**-f** | **--force**] [**-o** *PATH* | **--output** *PATH*] [**-l** | **--list**] [**--json**]

**craftpack build** [**-s** *PATH* | **--spec** *PATH*] [**-t** *TARGET* | **--target** *TARGET*] [**--package-version** *VERSION*] [**-o** *DIR* | **--output-dir** *DIR*] [**--arch** *ARCH*] [**--dry-run**] [**--strict**] [**--json**]

**craftpack validate** [**-s** *PATH* | **--spec** *PATH*] [**--strict**] [**--json**]

**craftpack** [**-V** | **--version** | **--version-info**] [**--json**]

**craftpack** [**-h** | **--help**]

## DESCRIPTION

**Craftpack** serves as the automated packaging factory (Component 4) within the Software Delivery Platform (SDP). It ingests pre-compiled application payloads, documentation, configuration templates, and declarative specification manifests (`craftpack.yml`), compiling them into immutable, system-compliant Linux distribution packages.

Designed around modern security, isolation, and portability principles, Craftpack operates entirely in pure Go (`CGO_ENABLED=0`) with zero external runtime dependencies—eliminating any reliance on host packaging tools such as **dpkg-deb**, **ar**, **tar**, or **gzip**.

### Core Architecture and Mechanics

* **Direct Binary Placement & Payload Isolation**: By default (`wrapper: false`), the compiled application executable is installed directly into `/usr/bin/<command>` with mode `0755`, eliminating unnecessary wrapper scripts for standalone compiled binaries. Auxiliary non-entrypoint assets are isolated in `/usr/lib/<app_id>/`. When the payload contains solely the entrypoint executable, `/usr/lib/<app_id>/` is completely omitted.
* **Transparent Proxy Launcher (Opt-In)**: When `wrapper: true` is configured, Craftpack isolates all payload files into `/usr/lib/<app_id>/` and synthesizes a lightweight, POSIX-compliant `/bin/sh` wrapper installed to `/usr/bin/<command>`. The launcher uses native POSIX `exec` semantics to replace its process image with the private payload binary, guaranteeing transparent signal handling, unbuffered stream passthrough, and elimination of shell injection risks.
* **Decoupled Manual Page Synthesis**: Markdown documentation files declared in the specification are compiled on-the-fly into roff manual formatting and compressed with gzip. Under the Zero-Markup policy, source Markdown remains clean and free of platform-specific headers or front-matter.
* **Shared Application Data & Templates**: Read-only architecture-independent assets and templates are deployed directly to `/usr/share/<app_id>/templates/` with mode `0644`.
* **Deterministic and Reproducible Builds**: Tar archive entries are sorted alphabetically, file modes are normalized (`0755` for directories/executables, `0644` for regular files), ownership is assigned to `root:root` (UID/GID 0), and timestamps honor the standard `SOURCE_DATE_EPOCH` environment variable.
* **Strict Stream Separation**: Clean, machine-readable data payloads (such as JSON summaries or version information) are emitted strictly to STDOUT. All diagnostic logs, progress notifications, warnings, and errors are routed to STDERR.
* **Autonomous Self-Packaging ($N \to N$)**: Craftpack packages itself using its own freshly compiled executable, eliminating external network bootstrap dependencies and ensuring offline assembly compliance.

## COMMANDS

* **init** [*TEMPLATE*]
  Scaffolds a new `craftpack.yml` packaging specification in the target directory using a predefined or user-created template. If no template name is specified, defaults to `deb`. Discovers templates through a 4-tier search cascade: environment override (`CRAFTPACK_TEMPLATES_DIR`), local workspace (`./templates/`), user custom directory (`$XDG_DATA_HOME/craftpack/templates/`), and system shared data (`/usr/share/craftpack/templates/`).

* **build**
  Compiles and bundles the target application into an immutable distribution package according to the specification manifest. The build process executes a strict 7-stage lifecycle pipeline:
  1. *Stage 1: CLI Ingestion & Schema Validation* - Parses input flags, evaluates workspace boundaries, and validates schema constraints.
  2. *Stage 2: Staging Area Setup & Payload Crawling* - Allocates an ephemeral workspace, crawls `payload_dir`, and stages the entrypoint directly to `/usr/bin/<command>` (or `/usr/lib/<app_id>/` when `wrapper: true`). Stages `templates_dir` to `/usr/share/<name>/templates/`.
  3. *Stage 3: Proxy Launcher Synthesis (Conditional)* - Synthesizes the `/usr/bin/<command>` proxy script using POSIX `exec` delegation when `wrapper: true`; bypassed in direct mode (`wrapper: false`).
  4. *Stage 4: Documentation Staging* - Compiles Markdown sources into compressed roff man pages under `/usr/share/man/man[1-8]/`.
  5. *Stage 5: Target Metadata Synthesis* - Generates target control files (`DEBIAN/control`, `conffiles`, maintainer hooks, and `md5sums`).
  6. *Stage 6: Archive Compilation* - Sequentially assembles archive layers into the target container (e.g. Unix `ar` container for `.deb`).
  7. *Stage 7: Release Manifest Generation & Staging Cleanup* - Computes SHA-256 digests, records `checksums.sha256`, and removes temporary staging assets.

* **validate**
  Parses and validates the project specification manifest (`craftpack.yml`) and asserts local workspace prerequisites without compiling or assembling distribution containers. Verifies package identity, SemVer versioning, maintainer syntax (RFC 822), homepage URI schemes, SPDX license identifiers, command collision blacklists, and workspace traversal boundaries.

## OPTIONS

### Global Options

* **-v**, **--verbose**
  Increase logging verbosity to debug level (-v: DEBUG, -vv: TRACE). May be combined with other log flags; flag precedence resolves using Last-Flag-Wins.

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

### Init Subcommand Options (`craftpack init`)

* **-o**, **--output** *PATH*
  Destination path where the generated manifest is written. Defaults to `./craftpack.yml`. Passing `-o -` streams the template content directly to STDOUT for inspection or piping.

* **-f**, **--force**
  Force overwriting the output file if it already exists. Without this flag, `craftpack init` aborts with an exit code of 1 if the target file exists.

* **-l**, **--list**
  List all available templates discovered across the template search paths, indicating their template name, filesystem location, and origin category (`[env]`, `[workspace]`, `[user]`, `[system]`).

* **--json**
  Format template list or creation output as structured JSON on STDOUT.

### Build Subcommand Options (`craftpack build`)

* **-s**, **--spec** *PATH*
  Filesystem path to the packaging specification manifest. Defaults to `craftpack.yml` within the active working directory.

* **-t**, **--target** *TARGET*
  Target packaging format. Currently supported: `deb` (Debian archive). This flag is mandatory during build execution.

* **--package-version** *VERSION*
  Semantic version string conforming strictly to SemVer 2.0.0 (e.g. `1.0.0`, `v2.1.0`, `0.9.0-rc.1+build.12`). Leading `v`/`V` prefixes are automatically normalized. This flag is mandatory during build execution.

* **-o**, **--output-dir** *DIR*
  Destination directory where compiled packages and `checksums.sha256` release manifests are written. Defaults to `./dist`. Parent directories are created automatically if nonexistent.

* **--arch** *ARCH*
  Target CPU architecture override (e.g. `amd64`, `arm64`, `all`, `x86_64`, `aarch64`). If omitted, defaults to the detected host CPU architecture.

* **--dry-run**
  Simulate all build stages (validation, staging, direct binary placement or launcher synthesis, metadata compilation) without creating container archives or release manifests on disk.

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

* **CRAFTPACK_TEMPLATES_DIR**
  Specifies the highest-priority template directory override in the discovery cascade, searched before workspace, user, and system template directories.

* **XDG_DATA_HOME**
  Base directory for user-specific data files (defaults to `~/.local/share`). User custom templates are searched in `$XDG_DATA_HOME/craftpack/templates/`.

* **CRAFTPACK_LOG_LEVEL**
  Specifies the default logging level if not overridden by command-line flags. Supported values: `trace`, `debug`, `info`, `warn`, `error`.

* **NO_COLOR**
  When set to any non-empty value, disables ANSI terminal escape codes and colorized logging output on STDERR.

* **SOURCE_DATE_EPOCH**
  Unix epoch timestamp (in seconds) used to set deterministic file modification times in archive headers, enabling bit-for-bit reproducible packaging builds.

## FILES

* **craftpack.yml**
  The declarative YAML specification describing application identity, payload boundaries, entrypoint, documentation, configurations, lifecycle hooks, and target-specific parameters. See **craftpack.yml(5)** for the complete schema reference.

* **/usr/share/craftpack/templates/**
  System shared directory containing distribution-installed built-in templates (such as `deb.yml`).

* **~/.local/share/craftpack/templates/**
  User custom templates directory where personal manifests can be saved for use with `craftpack init <name>`.

* **/usr/bin/<command>**
  Public command executable. In direct mode (`wrapper: false`, default), the compiled binary is placed directly here. In wrapper mode (`wrapper: true`), this is the synthesized proxy launcher script.

* **/usr/lib/<app_id>/**
  Isolated directory containing auxiliary assets, shared libraries, or the private executable when `wrapper: true` is configured (omitted for single-binary packages in direct mode).

* **/usr/share/man/man[1-8]/<name>.[1-8].gz**
  Compiled and gzipped Unix manual pages generated from source Markdown.

* **checksums.sha256**
  Cryptographic release manifest generated in the output directory recording SHA-256 digests and file sizes.

## EXIT STATUS

* **0**
  Success (`ExitSuccess`). Command completed execution successfully.

* **1**
  Validation or Boundary Error (`ExitValidation`). Triggered when configuration validation fails, required files are missing, destructive commands are detected in hooks, workspace traversal violations occur, or `craftpack init` target file already exists without `--force`.

* **2**
  CLI Usage or Syntax Error (`ExitUsage`). Triggered by invalid command syntax, unrecognized flags, missing mandatory flags, or extraneous arguments.

* **130**
  Execution Terminated (`ExitTerminated`). Process execution was interrupted by an operating system signal (such as `SIGINT` or `SIGTERM`).

## EXAMPLES

* Scaffold a new `craftpack.yml` in the current directory using the default Debian template:
  ```bash
  craftpack init
  ```

* Scaffold a Debian packaging specification explicitly:
  ```bash
  craftpack init deb
  ```

* List all available built-in and user templates:
  ```bash
  craftpack init --list
  ```

* Stream a template directly to standard output for piping:
  ```bash
  craftpack init deb -o -
  ```

* Force overwrite an existing specification file:
  ```bash
  craftpack init deb --force
  ```

* Validate a packaging specification in the current directory:
  ```bash
  craftpack validate
  ```

* Validate a specification in strict mode with JSON output:
  ```bash
  craftpack validate --spec craftpack.yml --strict --json
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
  ./dist/payload/bin/craftpack build --package-version 2.0.0 --target deb --output-dir ./dist
  ```

## AUTHORS

Written and maintained by **Marcin Kaim** <9829098+marcinkaim@users.noreply.github.com>.

## COPYRIGHT

Copyright (C) 2026 Marcin Kaim.
Free use of this software is granted under the terms of the GNU General Public License, Version 3 (GPLv3).

## SEE ALSO

**craftpack.yml(5)**, **dpkg(1)**, **deb(5)**, **deb-control(5)**, **ar(1)**, **gzip(1)**

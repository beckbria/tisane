# AGENTS.md

Guidelines and reference for AI agents and contributors working in the **Tisane** repository.

## Project Overview

Tisane is a high-speed crossword, cryptic, anagram, and dictionary pattern-matching suite inspired by (and binary-compatible with) classic Win32 TEA. It ships as a cross-platform desktop GUI (`tisane`) plus CLI utilities (`tisane-cli`, `tisane-build`, `tisane-extract`, `tisane-merge`).

---

## Keeping Documentation Up to Date

- **Maintain `AGENTS.md`**: Keep this file accurate as the codebase evolves. Whenever you encounter a build, test, cross-compilation, or runtime problem, add the steps to avoid or resolve it here to help future development.
- **Maintain `README.md` and `DEVELOPMENT.md`**: Update `README.md` whenever you make changes to user-facing CLI/UI behavior or installation, and update `DEVELOPMENT.md` whenever you make changes to project structure, dependencies, prerequisites, build commands, or test workflows.

---

## Important Architectural Decisions

### Multi-Platform First (Linux, Windows, macOS, Headless)
- **Pure-Go (`CGO_ENABLED=0`) by default**: All core libraries (`pkg/...`), CLI binaries, internal build tools (`internal/...`), and the Linux desktop GUI build without CGO. On Linux, Tisane uses Wails v3's PureGo backend (`-tags purego,gtk3` for GTK3/WebKitGTK 4.1, or `-tags purego` for GTK4/WebKitGTK 6.0), avoiding any dependency on `gcc`, `pkg-config`, or `-dev` system headers.
- **Cross-compilation from Linux to Windows**: Bazel targets (`//:windows`, `//:windows-release`, `//:windows-release-zip`, or `--config=windows`) cross-compile `windows/amd64` `.exe` binaries directly from Linux. Keep all code and build rules compatible with cross-compilation.
- **Platform-specific packaging & runtime details**:
  - **Windows**: `tisane.exe` is linked with `-H=windowsgui` (no background console window) and embeds a generated `.syso` PE resource (`cmd/tisane/internal/winrsrc`) containing the app icon, DPI/Common-Controls manifest, and version metadata. Releases are packaged as an NSIS installer (`//:windows-release` -> `tisane-setup.exe`, requires `makensis` on `PATH`) and a portable ZIP (`//:windows-release-zip`).
  - **Linux**: Releases (`//:linux-release` -> `linux-release.tar.gz`) bundle all binaries, dictionaries, `tisane.png`, and `tisane.desktop`.
  - **macOS & Headless**: Supported via native `WKWebView` and Wails v3 server mode (`-tags server`).
- **Platform-agnostic core**: Never assume Unix path separators, line endings, case-sensitive filesystems, or POSIX-only shell utilities in Go code or tests. Use `path/filepath`, `os.UserConfigDir()`, `os.PathListSeparator`, and `t.TempDir()`.

### Clean Separation of Concerns
- **Zero-dependency core libraries (`pkg/...`)**: Core packages (`tsd`, `pattern`, `search`, `lookup`, `profile`, `wordlist`) rely strictly on the Go standard library. Keep them decoupled from UI, CLI flag parsing, and OS-specific GUI frameworks.
- **Testable GUI backend (`cmd/tisane/app`)**: Desktop services (`DictionaryService`, `SearchService`, `LookupService`, `ProfileService`) interact with the Wails runtime solely through the `HostBridge` interface, allowing full unit and race testing without a display server or WebView.
- **Embedded Svelte 5 frontend**: `cmd/tisane/main.go` embeds `cmd/tisane/frontend/dist` via `//go:embed all:frontend/dist`. The frontend communicates with Go services through explicit Wails v3 fully-qualified method names (FQNs) in `cmd/tisane/frontend/src/lib/bindings.ts` behind a mockable `TisaneBackend` interface.
- **TEA compatibility**: Preserve binary compatibility with `TSD0`/`TSD1` `.tsd` dictionaries, `.tsm` alphabet maps, and legacy `TEACON.exe` switches in `tisane-cli`.

### Unified Build System (Bazel + Go + npm)
- **Bazel (Bzlmod)** (`MODULE.bazel`, `BUILD.bazel`, `release.bzl`, `.bazelrc`) is the primary orchestration layer for building, cross-compiling, testing, and packaging releases.
- Standard `go build` / `go test` and `npm` workflows must also continue to work directly.
- Whenever you add, remove, or change Go source files or imports, update the corresponding `BUILD.bazel` files (preserving `# keep` directives on customized rules such as `embedsrcs`, `select(...)`, and `gc_linkopts`).

---

## Repository Layout

| Path | Purpose |
| :--- | :--- |
| `BUILD.bazel`, `MODULE.bazel`, `release.bzl`, `.bazelrc` | Root Bazel/Bzlmod build graph, cross-platform targets, and release packaging rules |
| `artifacts/` | Design docs, specs, and implementation plans (`implementation_plan.md`, `pattern_syntax.md`, `dictionary_format.md`, `installer_plan.md`) |
| `build/` | Shared app icon (`appicon.png`), embedded Go asset package (`assets.go`), Linux `.desktop` entry, and Windows manifest/NSIS/version metadata (`build/windows/`) |
| `cmd/tisane/` | Desktop GUI entry point (`main.go`), Wails v3 wiring, and Bazel rules (`frontend.bzl`, `winrsrc.bzl`) |
| `cmd/tisane/app/` | Testable desktop backend services (`dict_service.go`, `tools_service.go`, `search_service.go`, `lookup_service.go`, `profile_service.go`, `dict_builder.go`, `wordlist_merge.go`, `events.go`, `state.go`) |
| `cmd/tisane/frontend/` | Svelte 5 + TypeScript + Tailwind CSS v4 + Vite + `@tanstack/svelte-virtual` frontend (`src/lib/bindings.ts`, `src/lib/types.ts`, `src/lib/stores/`, `src/lib/components/`) |
| `cmd/tisane/internal/` | Build-time helpers for `cmd/tisane` (`vitebuild` for frontend builds, `winrsrc` for Windows `.syso` generation) |
| `cmd/tisane-cli/` | CLI and interactive REPL search tool (POSIX flags + legacy `TEACON` switches) |
| `cmd/tisane-build/` | `.tsd` dictionary compiler and inspector (replaces `TSBuild.exe`) |
| `cmd/tisane-extract/` | `.tsd` word list and metadata extractor |
| `cmd/tisane-merge/` | Word list conversion and set-operation merge tool (replaces `TSMerge.exe`) |
| `dictionaries/` | Bundled open-source `.tsd` dictionaries included in release archives |
| `internal/buildutil/` | Shared destination path validation, regular-file staging, and version quad parsing |
| `internal/releasepkg/` | Cross-platform release archive packager (`.tar.gz` and `.zip`) invoked by Bazel |
| `internal/wininstaller/` | NSIS Windows installer builder wrapper invoked by Bazel |
| `pkg/tsd/` | Pure-Go `TSD0`/`TSD1` reader, streaming bit-unpacker, Huffman/zlib/RTF decoder, `.tsm` parser, exporter, and `TSD1` writer |
| `pkg/pattern/` | Macro expander, boolean expression parser, span validator, compiler, and backtracking VM |
| `pkg/search/` | Concurrent multi-dictionary word & Full-Text Search (FTS) engine with sorting and filtering |
| `pkg/lookup/` | Sanitized RTF-to-HTML/plain-text converter and `{entry}` web lookup URL builder |
| `pkg/profile/` | Portable TOML config (`config.toml`), dictionary discovery, and canonical `profile.Version` |
| `pkg/wordlist/` | Word list ASCII conversion and merge operations (`or`, `and`, `exclude`) |

---

## Coding Conventions & Best Practices

All code must follow idiomatic conventions and best practices for its language and framework:

### Go
- Format all Go code with `gofmt` and follow [Effective Go](https://go.dev/doc/effective_go) and Go Code Review Comments.
- Wrap errors with context using `fmt.Errorf("context: %w", err)` and handle errors explicitly (never silently discard errors).
- Propagate `context.Context` for cancellable work (especially searches, dictionary builds, and I/O).
- Write concurrency-safe code; all Go packages must pass `go test -race`.
- Prefer the Go standard library. Do not add third-party dependencies to `go.mod` without a clear, compelling reason; keep `pkg/...` free of external dependencies.
- Keep exported APIs concise, well-documented, and consistent with surrounding packages.

### TypeScript, Svelte 5 & Tailwind CSS
- Follow strict TypeScript (`strict: true` in `tsconfig.json`); avoid `any` and ensure `npm run check` (`svelte-check`) passes with zero errors or warnings.
- Use idiomatic **Svelte 5 runes** (`$state`, `$derived`, `$effect`, `$props`) rather than legacy Svelte 4 reactive declarations (`$:`, `export let`).
- Organize top-level application state into focused Svelte 5 `.svelte.ts` rune store factories under `cmd/tisane/frontend/src/lib/stores/` rather than growing `App.svelte`. Use `$state.raw` for large virtualized match arrays.
- Use Tailwind CSS v4 utility classes consistently and preserve responsive, accessible UI semantics (keyboard shortcuts, ARIA attributes, focus management).
- Keep large result lists virtualized via `@tanstack/svelte-virtual` so 100k+ hits stay responsive.
- Whenever a Go service method signature, FQN, or DTO JSON field changes in `cmd/tisane/app`, update `cmd/tisane/frontend/src/lib/bindings.ts`, `src/lib/types.ts`, and `cmd/tisane/main_test.go` in the same change—and rebuild `cmd/tisane/frontend/dist` (`npm run build`) if verifying the embedded desktop binary.

### Desktop UI & Wails v3 Conventions
- **Bound service method hygiene**: Every exported method on a service registered in `cmd/tisane/main.go` (`DictionaryService`, `ToolsService`, `SearchService`, `LookupService`, `ProfileService`) is automatically exposed to the WebView. Keep internal helpers unexported (such as `addDictionaryPaths` and `scanDirectory`) or as package-level functions (e.g., `app.ShutdownSearch`). Accept `ctx context.Context` as the first parameter on cancellable I/O methods (Wails v3 injects it automatically without changing JS signatures).
- **Typed event contracts & `searchId` correlation**: Define all event names in `cmd/tisane/app/events.go` and mirror their payload types in `WailsEventMap` (`cmd/tisane/frontend/src/lib/bindings.ts`). Because `StartSearch` streams asynchronously, the frontend generates and sends `searchId` up front so fast `search:batch`/`search:complete`/`search:cancelled` events cannot race the RPC return value.
- **Dictionary handle lifetimes**: `*tsd.Dictionary` handles in `app.State` are reference-counted (`acquire`/`release`). Never call `dict.Close()` directly on a dictionary that may still be held by an in-flight search or lookup; always use `SnapshotSearchDicts`/`SnapshotAllDicts` and `defer release()`.
- **Shortcut ownership**: Native Wails menu accelerators (`cmd/tisane/main.go`) own global desktop shortcuts (`CmdOrCtrl+W/F/B/D/Shift+B/Shift+M/,/1/2`, `F1`), while `App.svelte` owns `Escape` exclusively and gates its `<svelte:window>` fallback on `!isDesktopWebview()` so toggles never double-fire on desktop while server mode and Vitest remain testable.
- **Security invariants**:
  - Any URL passed to `HostBridge.OpenURL` (including `WebProvider.URLTemplate` in `OpenWebLookup` and `UpdateConfig`) must be validated to `http`/`https` (plus `mailto` for `OpenExternalURL`).
  - Any `{@html ...}` rendering (such as `DefinitionPane.svelte`) must only receive backend-sanitized HTML, and `cmd/tisane/frontend/index.html` must retain its strict Content Security Policy.
  - Never interpolate user-configurable strings (such as font families in `fonts.ts`) into inline `style` attributes without sanitizing CSS breakout characters (`;`, `{`, `}`, `(`, `)`, `\`).
- **Runtime pinning**: Keep `@wailsio/runtime` in `cmd/tisane/frontend/package.json` pinned to the exact Wails v3 version in `go.mod`.

### Bazel / Starlark
- Keep `BUILD.bazel` files accurate and minimal. Respect `# keep` comments so Gazelle does not clobber custom attributes.
- Bazel (`cmd/tisane:frontend_dist` via `@rules_nodejs`) builds the Svelte 5 frontend bundle hermetically from source using a pinned Node.js 22 toolchain, so Bazel releases never embed stale local `cmd/tisane/frontend/dist` artifacts. When verifying standalone `go build ./cmd/tisane`, refresh `cmd/tisane/frontend/dist` via `npm run build`.
- For `genrule`s that may execute on Windows as well as Linux/macOS, provide both `cmd` (POSIX) and `cmd_ps` (PowerShell).

### Commit Messages & Comments
- Keep both **commit messages** and **code comments** terse and focused on *why* rather than enumerating specific implementation details.
- **Commit messages**: Summarize intent concisely (e.g., prefer `"Dictionary Builder UI"` over `"Implement Dictionary Builder UI, reading the configurations from directory..."`).
- **Code comments**: Document rationale, trade-offs, or non-obvious invariants rather than restating what the code does.

---

## Versioning Policy

Tisane uses Semantic Versioning (`MAJOR.MINOR.PATCH`, e.g., `0.5.0`).

- **When to update**: Increment the build/version number whenever adding significant features or bug fixes.
- **Default increment**: Always increment the **patch** number by default (`0.5.0` -> `0.5.1`).
- **Major and minor updates**: Only update the **major** or **minor** version number when **explicitly requested by a human**.

### Single Source of Truth (`VERSION`)
The repository root [`VERSION`](VERSION) file is the single source of truth for the Tisane version across the entire codebase:

- **Go**: Root `version.go` (`package tisane`, Bazel target `//:version`) embeds `VERSION` via `//go:embed VERSION` and exposes `tisane.Version`. `pkg/profile.Version`, all CLI binaries (`tisane-cli`, `tisane-build`, `tisane-extract`, `tisane-merge`), `cmd/tisane`, `winrsrc`, and `wininstaller` all derive from `tisane.Version`.
- **Frontend (Vite / Svelte / TypeScript)**: `cmd/tisane/frontend/vite.config.ts` injects `__APP_VERSION__` from `TISANE_VERSION` (passed by `cmd/tisane/internal/vitebuild` during Bazel builds) or by reading `../../../VERSION` directly during local `npm` / `vitest` runs.
- When bumping the version, edit **only `VERSION`** (and rebuild `cmd/tisane/frontend/dist` via `npm run build` if verifying an embedded `go build` desktop binary).

---

## Writing and Running Tests

### Writing Tests
- **Go unit & integration tests (`*_test.go`)**:
  - Prefer table-driven tests with clear subtest names (`t.Run(...)`).
  - Isolate filesystem and environment state with `t.TempDir()` and `t.Setenv(...)` (`TISANE_CONFIG`, `TISANE_DICT_PATH`, `BUILD_WORKING_DIRECTORY`); never rely on user home directories or external host paths (construct synthetic word lists or `.tsd` fixtures via `tsd.BuildBytes`/`tsd.BuildFile` in tests).
  - Test `cmd/tisane/app` services by injecting a mock `HostBridge` rather than starting a live Wails window.
  - Test CLI tools (`cmd/tisane-*`) by invoking their runnable entry functions with captured `stdout`/`stderr` buffers and checking exit codes.
  - Add regression tests for every bug fix and unit tests for every new feature.
- **Frontend tests (`cmd/tisane/frontend/src/*.test.ts`)**:
  - Use **Vitest** + **@testing-library/svelte** + **@testing-library/jest-dom** under `jsdom` (configured in `vite.config.ts` and `src/test-setup.ts`).
  - Inject a mock `TisaneBackend` when mounting components so tests are fast, deterministic, and independent of the Go/Wails runtime.
  - Cover user interactions, state transitions, and virtualization behavior where relevant.

### Running Tests

#### All Go Tests via Bazel (Recommended)
```bash
bazel test //...
```

#### Go Tests with Race Detector
```bash
# Core libraries, CLIs, desktop backend services, and internal build tools
go test -race ./pkg/... ./cmd/tisane-build ./cmd/tisane-extract ./cmd/tisane-merge ./cmd/tisane-cli ./cmd/tisane/app ./internal/...

# Wails v3 service FQN bindings & embedded asset server check (Linux PureGo)
CGO_ENABLED=0 go test -tags purego,gtk3 -v ./cmd/tisane
```

#### Frontend Type-Check & Component Tests
```bash
# If node/npm are not on host PATH, add Bazel's hermetic Node 22 toolchain first:
# export PATH="$(bazel info output_base)/external/rules_nodejs++node+nodejs_linux_amd64/bin:$PATH"
cd cmd/tisane/frontend
npm run check   # svelte-check + strict TypeScript check
npm test        # Vitest suite
```

#### Cross-Platform Build Verification
When touching build files, platform-specific code, or the frontend bundle, verify that builds succeed across targets:
```bash
# Rebuild embedded frontend bundle (when frontend sources changed)
(cd cmd/tisane/frontend && npm run build)

# Verify native Linux and cross-compiled Windows binaries
bazel build //:linux //:windows
```

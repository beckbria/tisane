# Developing Tisane

This guide covers prerequisites, repository structure, building from source, cross-compiling releases, and running tests for Tisane. For end-user installation and usage instructions (Desktop GUI and CLI tools), see [README.md](README.md).

## Prerequisites

Installing `bazel` (or [Bazelisk](https://github.com/bazelbuild/bazelisk) on Windows) will manage Go dependencies automatically.

To build the Windows installer (`bazel build //:windows-release`), **NSIS** (`makensis`) must also be installed and available on `PATH`:

- **Linux (Ubuntu / Debian)**: `sudo apt install nsis`
- **Linux (Fedora)**: `sudo dnf install nsis`
- **macOS**: `brew install nsis`
- **Windows**: `winget install NSIS.NSIS` (or `choco install nsis`)

*(Note: `//:linux-release`, `//:windows`, and `//:windows-release-zip` do not require NSIS.)*

### Frameworks Used in Tisane

1. **Go 1.26+** (`go version`, managed hermetically when building with Bazel)
2. **Node.js 22+ and npm** (managed hermetically via `rules_nodejs` during Bazel builds; required on `PATH` only for standalone `npm` / `go build ./cmd/tisane` workflows)
3. **Desktop WebView runtime** (for running the `tisane` desktop window):
   - **Linux (Ubuntu / Debian)**: `libgtk-3-0` and `libwebkit2gtk-4.1-0` (installed by default on most desktop distributions).
     - *Note*: Tisane supports Wails v3's **PureGo** Linux backend (`-tags purego,gtk3`), so **no C compiler (`gcc`), `pkg-config`, or `-dev` header packages** are required to build on Linux.
     - On GTK4 + WebKitGTK 6.0 distributions, use `-tags purego` instead of `-tags purego,gtk3`.
   - **macOS**: Built-in `WKWebView` (Xcode Command Line Tools if building with `cgo`).
   - **Windows**: Built-in Microsoft Edge `WebView2` runtime.

---

## Repository Layout

| Path | Description |
| :--- | :--- |
| `artifacts` | Design documents and work plans |
| `build` | Shared build assets (`appicon.png`) and platform-specific manifests/metadata (`windows/`, `linux/`) |
| `cmd/tisane` | **Desktop GUI application** (Go + Wails v3 + Svelte 5 + Tailwind CSS v4 + `@tanstack/svelte-virtual`, with Dark/Light/System themes, UI zoom, and WCAG AA accessibility) |
| `cmd/tisane-cli` | **Command-line & interactive REPL search tool** (compatible with legacy `TEACON` switches plus JSON/CSV, FTS, `--define`, and `--enum`) |
| `cmd/tisane-build` | **Dictionary compiler & inspector** (replaces `TSBuild.exe`; compiles plain-text/RTF word lists into `.tsd` files) |
| `cmd/tisane-extract` | **Word list & metadata extractor** (exports `.tsd` dictionaries to plain one-word-per-line lists or `tisane-build` source files) |
| `cmd/tisane-merge` | **Word list merger** (replaces `TSMerge.exe`; ASCII conversion and `or`/`and`/`exclude` set operations) |
| `internal/` | Release archive packager (`releasepkg`), NSIS installer builder (`wininstaller`), atomic file/CLI helpers (`fsutil`), and shared build helpers (`buildutil`) |
| `pkg/tsd` | Pure-Go reader, streaming bit-unpacker, RTF/zlib definition decoder, and byte-accurate `TSD1` writer |
| `pkg/pattern` | Macro expander, boolean expression parser, live span validator, calibration-sorted compiler, and backtracking VM |
| `pkg/search` | Concurrent multi-dictionary word & indexed/sequential Full-Text Search (FTS) engine with 6 sort modes |
| `pkg/lookup` | Sanitized `.tsd` RTF-to-HTML / plain-text converter and `{entry}` web lookup URL builder |
| `pkg/profile` | Portable configuration (`~/.config/tisane/config.toml`) and `.tsd` dictionary discovery |
| `pkg/wordlist` | Streaming word list ASCII transliteration and set-operation merger |

---

## Building and Running the Desktop UI (`tisane`)

The `tisane` desktop binary embeds the compiled Svelte 5 frontend from `cmd/tisane/frontend/dist` via Go's `//go:embed`. Always build the frontend assets at least once before compiling `./cmd/tisane`.

### Step 1: Build the Svelte 5 Frontend Bundle

```bash
cd cmd/tisane/frontend
npm install
npm run build
cd ../../..
```

This outputs the production HTML/CSS/JS bundle into `cmd/tisane/frontend/dist/`.

### Step 2: Compile the `tisane` Executable

Choose the command matching your platform:

#### Linux (Recommended — PureGo, zero `-dev` headers needed)
```bash
# GTK3 + libwebkit2gtk-4.1 (Ubuntu 22.04 / 24.04 / 26.04, Debian, Mint, Fedora)
CGO_ENABLED=0 go build -tags purego,gtk3 -o tisane ./cmd/tisane

# Or GTK4 + libwebkitgtk-6.0
CGO_ENABLED=0 go build -tags purego -o tisane ./cmd/tisane
```

#### Linux (Traditional CGO build, if `libgtk-3-dev` & `libwebkit2gtk-4.1-dev` are installed)
```bash
CGO_ENABLED=1 go build -o tisane ./cmd/tisane
```

#### macOS & Windows
```bash
go build -o tisane ./cmd/tisane
# On Windows (PowerShell):
# go build -ldflags "-H=windowsgui" -o tisane.exe ./cmd/tisane
```

#### Headless / Browser Server Mode (Any OS, no GUI libraries needed)
Wails v3 can also serve the exact same backend and UI over HTTP for trusted local browser use:
```bash
CGO_ENABLED=0 go build -tags server -o tisane-server ./cmd/tisane
./tisane-server
# Open http://localhost:8080 in any browser
```
> **Security note**: Server mode shares a single `app.State` across connected clients and has no built-in HTTP authentication. By default, `-tags server` disables host filesystem-mutation tools (Dictionary Builder, Word List Merger, adding/removing dictionaries, config reset) and server-side `OpenURL` launches; set `TISANE_SERVER_ALLOW_HOST_IO=1` only in trusted single-user environments, and place an authenticating reverse proxy in front if exposing beyond `localhost`.

### Step 3: Run `tisane`

```bash
./tisane
```

You can optionally point Tisane at one or more directories of `.tsd` files or a custom config file via environment variables:

```bash
TISANE_DICT_PATH="/path/to/Dictionaries:/another/folder" ./tisane
```

- **First-run discovery**: On first launch, Tisane automatically scans `TISANE_DICT_PATH`, the directory the executable is running from, the working directory, and the `dictionaries` subdirectory for `.tsd` files, and saves your profile to `~/.config/tisane/config.toml` (Linux/macOS) or `%AppData%\tisane\config.toml` (Windows). Override the config file path at any time with `TISANE_CONFIG=/path/to/config.toml`.

## Live Frontend Hot-Reload (UI Development Workflow)

When iterating on Svelte components in `cmd/tisane/frontend/src`:

```bash
# Terminal 1: Start the Vite dev server (defaults to http://localhost:5173)
cd cmd/tisane/frontend
npm run dev

# Terminal 2: Launch Tisane pointed at the Vite dev server
FRONTEND_DEVSERVER_URL=http://localhost:5173 ./tisane
```

## Building the CLI Tools

```bash
# CLI & interactive REPL search tool
CGO_ENABLED=0 go build -o tisane-cli ./cmd/tisane-cli

# Dictionary builder
CGO_ENABLED=0 go build -o tisane-build ./cmd/tisane-build

# Dictionary extractor
CGO_ENABLED=0 go build -o tisane-extract ./cmd/tisane-extract

# Word list merger
CGO_ENABLED=0 go build -o tisane-merge ./cmd/tisane-merge
```

## Building with Bazel (Linux & Windows Cross-Compilation)

The repository includes Bzlmod (`MODULE.bazel`) and `rules_go` targets to build Linux binaries, cross-compile Windows `.exe` binaries from inside Linux, and run all tests:

```bash
# Build native Linux binaries (tisane, tisane-cli, tisane-build, tisane-extract, tisane-merge)
bazel build //:tisane //:tisane-cli //:tisane-build
# Or build the entire Linux binary group:
bazel build //:linux

# Cross-compile Windows (.exe) binaries from inside Linux
bazel build //:tisane-windows //:tisane-cli-windows //:tisane-build-windows
# Or build the entire Windows binary group:
bazel build //:windows
# Or cross-compile any target via the windows config / platform flag:
bazel build --config=windows //:tisane //:tisane-cli //:tisane-build

# Build distributable releases (all binaries + bundled dictionaries):
# - bazel-bin/linux-release.tar.gz (Linux binaries + dictionaries/)
# - bazel-bin/tisane-setup.exe     (Windows NSIS installer; requires NSIS / `makensis` — see Prerequisites)
# - bazel-bin/windows-release.zip  (Portable Windows .exe binaries + dictionaries/)
bazel build //:linux-release //:windows-release //:windows-release-zip

# Run all Go unit and integration tests via Bazel
bazel test //...
```

## Running Tests & Verification

### Go Unit, Integration & Race Tests

```bash
# Core libraries, CLIs, desktop backend services, and internal build tools (with race detector)
go test -race ./pkg/... ./cmd/tisane-build ./cmd/tisane-extract ./cmd/tisane-merge ./cmd/tisane-cli ./cmd/tisane/app ./internal/...

# Wails v3 service FQN bindings & embedded asset server check (Linux PureGo)
CGO_ENABLED=0 go test -tags purego,gtk3 -v ./cmd/tisane
```

### Frontend Type-Check & Vitest Component / Virtualization Suite

```bash
cd cmd/tisane/frontend
npm run check   # svelte-check + TypeScript strict verification
npm test        # Vitest + @testing-library/svelte (includes 200k-item multi-pane virtualization test)
```

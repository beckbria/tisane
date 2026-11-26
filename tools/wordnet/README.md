# `tools/wordnet` — Open English WordNet Dictionary Generator

Extracts all words (`<Lemma writtenForm="...">` and `<Form writtenForm="...">`) from Open English WordNet LMF XML dumps, optionally normalizes them via `pkg/wordlist`, and compiles a Tisane `.tsd` dictionary via `pkg/tsd`.

> **Note:** This is a manually invoked developer/maintainer tool. It is tagged `manual` in Bazel and never runs during normal `tisane` builds or releases. When using Open English WordNet data, please mind their license at https://en-word.net/.

## Usage

Run directly with Go or Bazel from the repository root:

### 1. Download and build the latest Open English WordNet dictionary

```bash
go run ./tools/wordnet
# or:
bazel run //tools/wordnet
```

- Checks `https://en-word.net/downloads` for the latest Open English WordNet LMF XML dump, preferring a `plus` edition (e.g., `english-wordnet-2025-plus.xml.gz`) over a standard edition (e.g., `english-wordnet-2025.xml.gz`).
- Downloads the `.xml.gz` dump to a temporary directory (deleted after processing unless `--keep-dump` is specified).
- Extracts all unique words and compiles `<dump-name>.tsd` (e.g., `english-wordnet-2025-plus.tsd`).

### 2. Build from an already-downloaded local dump

```bash
go run ./tools/wordnet english-wordnet-2025-plus.xml.gz
```

Writes `english-wordnet-2025-plus.tsd`.

### 3. Extract `.txt` only (skip `.tsd` compilation)

```bash
go run ./tools/wordnet --txt-only english-wordnet-2025-plus.xml.gz
```

## Options

- `-i, --input`: Local already-downloaded WordNet LMF XML dump file (`.xml.gz` or `.xml`).
- `-u, --url`: Remote WordNet downloads page or direct dump URL (default: `https://en-word.net/downloads`).
- `-o, --output`: Output `.tsd` file path (or `.txt` when `--txt-only` is set; defaults to `<dump-basename>.tsd`).
- `--work-dir`: Directory for downloaded dump file and temporary staging files.
- `--keep-dump`: Preserve downloaded dump file after processing instead of deleting.
- `--convert`: Optional word list conversion applied before building: `none` (default), `ascii`, `ascii-letters`, `ascii-lower`, or `ascii-upper`.
- `--desc`: Dictionary description stored in the `.tsd` header (defaults to `Open English WordNet (<dump-basename>)`).
- `--keep-txt`: Preserve intermediate `.txt` word list alongside the `.tsd` output.
- `--txt-only`: Write only the `.txt` word list without compiling a `.tsd` dictionary.

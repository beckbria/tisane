# `tools/wikipedia` — Wikipedia Dictionary Generator

Extracts Wikipedia article titles from Wikimedia page-title dumps (`all-titles-in-ns-0.gz`) or MediaWiki XML dumps, deduplicates them on normalized keys via `pkg/wordlist` while preserving proper names, and compiles a Tisane `.tsd` dictionary via `pkg/tsd`.

> **Note:** This is a manually invoked developer/maintainer tool. It is tagged `manual` in Bazel and never runs during normal `tisane` builds or releases.

## Usage

Run directly with Go or Bazel from the repository root:

### 1. Download, deduplicate, and build the latest English Wikipedia dictionary

```bash
go run ./tools/wikipedia
# or:
bazel run //tools/wikipedia
```

- Prompts for a contact URL and email address (validated) to construct the Wikimedia-compliant `User-Agent`: `TisaneDictionary/0.0 (<url>; <email>)`.
- Resolves the latest `enwiki-<YYYYMMDD>-all-titles-in-ns*` dump from `https://dumps.wikimedia.org/other/pagetitles/`.
- Displays the download plan and requires explicit `y`/`yes` confirmation before downloading.
- Downloads using a single stream and keeps at most two shard files on disk at any time (deleting each shard after processing unless `--keep-dump` is passed).
- Sorts extracted titles into three priority tiers (preserving input order within each tier):
  1. Basic titles containing only `[a-zA-Z '!?,/.-]`
  2. Other titles without ASCII digits `[0-9]`
  3. Titles containing ASCII digits `[0-9]`
- Deduplicates the tier-sorted word list on normalized keys (`ascii-lower` by default via `pkg/wordlist`) so the earliest, highest-priority proper name (e.g., `Galaxy Quest`, `West Hall`) is preserved while logical duplicates (`galaxyquest`, `Westhall`, `Galaxy Quest 2`) are dropped, then compiles `<prefix>-<YYYYMMDD>.tsd` using `pkg/tsd`.

### 2. Build from an already-downloaded dump

```bash
go run ./tools/wikipedia enwiki-20261116-all-titles-in-ns-0.gz
```

By default, deduplication compares titles using their `ascii-lower` normalized form while preserving the original proper name (e.g., `Galaxy Quest` and `galaxyquest` collapse to `Galaxy Quest`; `West Hall` and `Westhall` collapse to `West Hall`). To deduplicate only exact title matches instead, pass `--convert none`:

```bash
go run ./tools/wikipedia --convert none enwiki-20261116-all-titles-in-ns-0.gz
```

### 3. Extract `.txt` only (skip `.tsd` compilation)

```bash
go run ./tools/wikipedia --txt-only enwiki-20261116-all-titles-in-ns-0.gz
```

## Options

- `-i, --input`: Local already-downloaded dump file(s) (comma-separated).
- `-u, --url`: Remote Wikimedia dump URL or directory index URL.
- `-o, --output`: Output `.tsd` file path (or `.txt` when `--txt-only` is set; dump date is enforced in the filename).
- `--date`: Override dump date (`YYYYMMDD`) for output filename.
- `--work-dir`: Directory for temporary downloaded shards and intermediate word lists.
- `--keep-dump`: Preserve downloaded dump file(s) after processing instead of deleting.
- `--convert`: Normalization applied to deduplication keys while preserving original titles: `ascii-lower` (default), `none`, `ascii`, `ascii-letters`, or `ascii-upper`.
- `--desc`: Dictionary description stored in the `.tsd` header (defaults to `Wikipedia article titles (<prefix> <YYYYMMDD>)`).
- `--no-merge`: Skip the title deduplication step.
- `--keep-txt`: Preserve intermediate extracted/deduplicated `.txt` file(s) alongside the `.tsd` output.
- `--txt-only`: Stop after writing the `.txt` word list without compiling a `.tsd` dictionary.

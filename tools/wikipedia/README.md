# `tools/wikipedia` — Wikipedia Dictionary Generator

Extracts Wikipedia article titles from Wikimedia page-title dumps (`all-titles-in-ns-0.gz`) or MediaWiki XML dumps, deduplicates them via `pkg/wordlist`, and compiles a Tisane `.tsd` dictionary via `pkg/tsd`.

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
- Deduplicates the word list using `pkg/wordlist` (`or` union merge with optional `--convert` normalization) and compiles `<prefix>-<YYYYMMDD>.tsd` using `pkg/tsd`.

### 2. Build from an already-downloaded dump

```bash
go run ./tools/wikipedia enwiki-20261116-all-titles-in-ns-0.gz
```

To collapse logical duplicates that differ only by spacing, punctuation, diacritics, or casing (e.g., `West Hall` vs. `Westhall`), pass `--convert`:

```bash
go run ./tools/wikipedia --convert ascii-lower enwiki-20261116-all-titles-in-ns-0.gz
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
- `--convert`: Word list conversion applied during deduplication: `none` (default), `ascii`, `ascii-letters`, `ascii-lower`, or `ascii-upper`.
- `--desc`: Dictionary description stored in the `.tsd` header (defaults to `Wikipedia article titles (<prefix> <YYYYMMDD>)`).
- `--no-merge`: Skip the `pkg/wordlist` deduplication/conversion step.
- `--keep-txt`: Preserve intermediate extracted/deduplicated `.txt` file(s) alongside the `.tsd` output.
- `--txt-only`: Stop after writing the `.txt` word list without compiling a `.tsd` dictionary.

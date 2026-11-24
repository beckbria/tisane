# <img src="./build/appicon.png" alt="Tisane icon" width="80" align="center" /> Tisane

**Tisane** is a pattern matching/regex tool. It provides high-speed crossword, cryptic, anagram, and dictionary search. It can operate on custom user-defined dictionaries.

![Screenshot of Tisane UI](./img/tisane_ui.png)

## Installing

Installing Tisane is done by downloading the latest release from the [releases page](https://github.com/beckbria/tisane/releases) and extracting the archive to a location on your computer.

## Using Tisane

### Dictionaries

A limited number of publicly available dictionaries are included in the releases. If you own a copy of TEA, dictionaries that came with it (found in `C:\Program Files (x86)\Crossword Man\TEA Crossword Helper 2.11\Dictionaries\`) can be used with Tisane. Some other dictionaries can be found in the [tisane-dictionaries repository](https://github.com/beckbria/tisane-dictionaries).

On first launch, Tisane automatically scans `TISANE_DICT_PATH`, the directory the executable is running from, the working directory, and the `dictionaries` subdirectory for `.tsd` files, and saves your profile to `~/.config/tisane/config.toml` (Linux/macOS) or `%AppData%\tisane\config.toml` (Windows). You can point Tisane at additional dictionary directories via `TISANE_DICT_PATH` (e.g., `TISANE_DICT_PATH="/path/to/Dictionaries:/another/folder" ./tisane`) or override the config file path with `TISANE_CONFIG=/path/to/config.toml`.

### Pattern Syntax Summary

| Syntax | Meaning | Example |
| :--- | :--- | :--- |
| `.` | Any single letter | `c.o..w.rd` → `crossword` |
| `?` | Any single letter (alias for `.`) | `c?o??w?rd` → `crossword` |
| `*` | Zero or more letters | `anti*dis*` |
| `[abc]` / `[^abc]` | Character choice list / negated choice list | `[^aeiou]at` → `bat`, `cat`, … |
| `;letters` | Anagram letter pool (usable with `.`, `?`, `*`) | `;listen` → `enlist`, `silent`, `tinsel` |
| `template;pool` | Template constrained by anagram pool | `m.....r;aadmort*` → `matador` |
| `(sub)` | Contiguous subpattern (can be anagrammed with `;`) | `*(cat)*` or `*(;act)*` |
| `0`–`9` | Positional equality variables | `123321` (6-letter palindromes) |
| `@` / `#` | User macros (defaults: `@` = `[aeiou]`, `#` = `[^aeiou]`) | `@#@#@#` |
| `&` `\|` `!` `( )` | Boolean AND, OR, NOT, and grouping | `;retinas* & !s*` |
| `\` | Escape special character literally | `\-\'` |

### Desktop UI Guide & Keyboard Shortcuts

- **Dictionary Sidebar (Left)**:
  - Click **+ Add** (`Ctrl+O`) to register `.tsd` dictionary files via the native file picker.
  - Toggle checkboxes to enable or disable dictionaries for the active search.
  - **Drag and drop** rows (`≡`) or click **`↑` / `↓`** to set dictionary priority order (controls tiled pane order, `Dictionary Order` sort, and cross-dictionary duplicate suppression).
  - Badges indicate total word count plus `defs` (local definitions) and `fts` (Full-Text Search index) capabilities.
- **Results Workspace (Center)**:
  - **Separate Tiled Panes (`⊞ Tiled`, default)**: Displays an independently virtualized scrollable pane with match counts for each enabled dictionary side-by-side (automatically adapting column count to container width).
  - **Unified Merged List (`≡ Unified`)**: Merges hits across all enabled dictionaries into a single virtualized list with `[Dictionary]` source tags.
  - Both modes handle 100,000+ streaming matches smoothly, display optional annotations, `-OMITTED` / `+ADDED` anagram balance pills, and local-definition indicators, and let you **Copy** all matched words in a pane or press `Ctrl+C` / `Cmd+C` on a focused result list to copy the selected headword.
- **Pattern & Filter Bars (Top)**:
  - Switch between Word Search and Full Text (FTS) modes.
  - Live validation previews matched word-length bounds or highlights exact syntax error spans as you type.
  - Constrain **Enum** (e.g., `4,3` or `4-2-2` in Word Search mode), **Min / Max Length** (`1–100`, click `–` to pin `=` exact length), **Entry Categories** (`abc` lowercase, `Abc` capitalized, `a-b` punctuated, `a b` phrases), **Dedupe**, and **Sort** (`Dictionary Order`, `Smart Balance`, `A → Z`, `Z → A`, `Length ↑`, `Length ↓`).
  - Open **Preferences** (`Ctrl+,`) or the **View** menu to switch between **Dark**, **Light**, and **System** color themes, adjust **UI Scale** (`75%–200%`), or customize per-element fonts and sizes.
- **Definitions & Web Search Fallback (Bottom)**:
  - Click any matched word to view sanitized RTF definitions across your dictionaries, or click **Copy** in the definition header to copy plain-text definitions to the clipboard.
  - Drag the top divider to resize the definition pane, or collapse/expand it (`Ctrl+D` or the toolbar **Defs** toggle) to give more vertical space to the results panes. Double-clicking a match while collapsed re-opens the definition pane.
  - Double-click any word inside a definition to cross-lookup that word; navigate lookup history with the arrow buttons.
  - Toggle **Include mentions** to also surface entries where the word appears in the definition body via FTS.
  - When no local definition is available (or from the header bar at any time), click on a dictionary provider to do a web search. You can configure your own custom `{entry}` URL templates in Settings.

| Shortcut | Action |
| :--- | :--- |
| `Ctrl+W` / `Cmd+W` | Switch to **Word Search** mode and focus pattern bar |
| `Ctrl+F` / `Cmd+F` | Switch to **Full Text Search (FTS)** mode and focus pattern bar |
| `Enter` | Start search |
| `Escape` | Cancel active search (or close open modal/popover) |
| `Alt+Down` | Open recent queries dropdown |
| `Ctrl+1` / `Cmd+1` | Switch results to **Separate Tiled Panes** (`⊞ Tiled`) |
| `Ctrl+2` / `Cmd+2` | Switch results to **Unified Merged List** (`≡ Unified`) |
| `Ctrl+B` / `Cmd+B` | Toggle **Dictionaries** sidebar panel |
| `Ctrl+D` / `Cmd+D` | Toggle **Definitions** bottom panel |
| `Ctrl+=` / `Cmd+=` | Zoom UI in (`75%–200%`) |
| `Ctrl+-` / `Cmd+-` | Zoom UI out (`75%–200%`) |
| `Ctrl+0` / `Cmd+0` | Reset UI zoom to `100%` |
| `Ctrl+O` / `Cmd+O` | Add `.tsd` dictionary files |
| `Ctrl+,` / `Cmd+,` | Open **Preferences** (themes, UI zoom, fonts, macros & web search providers) |
| `F1` | Toggle **Pattern Syntax Quick Reference** |
| `Up` / `Down` / `Home` / `End` / `PgUp` / `PgDn` | Navigate virtualized results and update definition |
| `Ctrl+C` / `Cmd+C` (in pane) | Copy selected matched word to clipboard |

### CLI

`tisane-cli` accepts both modern POSIX flags and legacy `TEACON.exe` switches (`/D:`, `/S:`, `/L:`, `/A:`, `/H:`, `/B:`, `/N:`, `/O:`, `/X:`, `/U:`, `/Q`, `/?`). It can be used for a single query or in an interactive mode (`-i`) and can output plain text or structured formats.

```bash
# Basic crossword template & anagram query against specific dictionaries
./tisane-cli -d "Core English;The Devil's Dictionary" "c.o..w.rd"

# Sub-anagram search sorted by Smart Balance (exact anagrams first)
./tisane-cli -d "Core English" --sort smart -b ".....*;aadmort"

# Crossword enumeration shorthand (4-letter word, space, 3-letter word)
./tisane-cli -d "Core English" --enum 4,3 ";icecream*"

# Exclude multi-word phrases and proper nouns
./tisane-cli -d "Core English" --no-phrases --no-proper "c.t*"

# Full-Text Search (FTS) across dictionary definitions
./tisane-cli -d "The Devil's Dictionary" -F "blackguard & vision"

# View formatted terminal definitions (prints web lookup URLs if no local definition exists)
./tisane-cli -d "The Devil's Dictionary" --define cynic

# Structured JSON, JSONL, or CSV output for scripting / jq
./tisane-cli -d "Core English" --format json --dedupe --show-dict ";listen"

# Interactive REPL mode with command history and :commands (:help, :dicts, :len, :fts, :def)
./tisane-cli -d "Core English;The Devil's Dictionary" -i

# Legacy TEACON-compatible batch/switch invocation
./tisane-cli "/D:Core English" /S:7 /L:7 /O:4 ";aadmort"
```

Run `./tisane-cli --help` for the complete list of flags.

### Dictionary Builder (`tisane-build`)

`tisane-build` compiles UTF-8 or Windows-1252 word/definition text files into compressed `TSD1` `.tsd` dictionaries (with optional `.tsm` custom alphabet maps and inverted FTS indexes), or inspects headers of existing `.tsd` files.

```bash
# Inspect metadata, version, capabilities, and word counts of existing .tsd files
./tisane-build --info "Dictionaries/Core English.tsd" "Dictionaries/The Devil's Dictionary.tsd"

# Compile a plain word list or | / RTF definition file into a .tsd dictionary
./tisane-build -d "Custom Crossword List" words.txt custom.tsd

# Compile using a custom .tsm alphabet/transliteration map and TSV annotations
./tisane-build --map Maps/French.TSM --tsv -i lexicon.tsv -o french.tsd
```

#### Input File Syntax Summary
- **Simple word list**: One entry per line, optional `;annotation` (or `\tannotation` with `--tsv`).
- **Definitions**: `headword;optional_qualifier|definition text` (supports plain text or `{\rtf1 ...}` RTF blocks; multiple headwords/aliases sharing one definition can be separated by `|` before the final definition segment, matching `TSBuild` format).

Run `./tisane-build --help` for all builder flags (`--min-len`, `--max-len`, `--max-fts-pct`, `--no-sort`, `--encoding`).

### Dictionary Extractor

`tisane-extract` exports words from any `.tsd` (`TSD0` or `TSD1`) dictionary either as a clean one-word-per-line list (`--simple`, default) or as a `tisane-build`-compatible metadata source file (`--metadata`, preserving description headers, `+annotations`, `+qualifiers`, `:shared` headwords, `;aliases`, and `|definitions`).

```bash
# Simple mode (default): one word per line to stdout or file, discarding definitions & annotations
./tisane-extract "Dictionaries/Core English.tsd" > words.txt
./tisane-extract --simple "Dictionaries/The Devil's Dictionary.tsd" devils-words.txt

# Metadata mode: full tisane-build / TSBuild source format (headwords, annotations, aliases, definitions)
./tisane-extract --metadata "Dictionaries/The Devil's Dictionary.tsd" devils-source.txt

# Metadata without definitions: keep word+annotation pairs only, discarding definitions & qualifiers
./tisane-extract --metadata --no-defs "Dictionaries/tsbuild2.tsd" annotated-words.txt

# Filter extracted words by canonical letter length (e.g. 5-letter words only) and stream by length bucket
./tisane-extract --min-len 5 --max-len 5 --sort length "Dictionaries/Core English.tsd" fives.txt

# Also extract the dictionary's embedded .tsm alphabet map for recompilation with tisane-build
./tisane-extract --metadata --save-map custom.tsm custom.tsd custom-source.txt
```

Run `./tisane-extract --help` for all extractor options (`--no-header`, `--no-dedupe`, `--sort`, `--encoding`).

### Word List Merger (`tisane-merge`)

`tisane-merge` converts and combines two plain-text word lists using set operations (`or` union, `and` intersection, `exclude` difference) after optional ASCII transliteration/case normalization (`none`, `ascii`, `ascii-letters`, `ascii-lower`, `ascii-upper`).

```bash
# Intersect two lists after ASCII lowercase normalization
./tisane-merge --op and --convert ascii-lower list1.txt list2.txt common.txt

# Subtract secondary words from a primary list and print to stdout
./tisane-merge --op exclude -c ascii-letters primary.txt blacklist.txt -
```

Run `./tisane-merge --help` for all merger options.

## Pattern Matching Tools

### Comparison to TEA

Tisane takes significant inspiration from (and owes great thanks to) the classic Win32 **TEA** by Ross Beresford ([Crossword Man](http://www.rossberesford.com/crosswordman)). TEA is no longer available for purchase, but Tisane provides a modern UI compatible for TEA users. The dictionary files included with TEA are not included in this repository.

Tisane's query syntax is borrowed from TEA and its dictionary files are identical. If you purchased TEA and have dictionary files created for it, those files can be used with Tisane.

* `tisane-cli` respects the CLI switches from `TEACON.EXE` and can be used as a drop-in replacement. 
* `tisane-build` is a command-line tool to build new dictionary files from word lists
* `tisane` is a desktop GUI application whose default interface is similar to that of TEA

Tisane contains features and performance improvements not included in TEA, described below.

### Comparison to Nutrimatic

[Nutrimatic](https://nutrimatic.org) is another excellent modern pattern searching engine which makes a different tradeoff between exhaustive search and ranking.

Nutrimatic ranks likely words and multi-word phrases utilizing usage frequency data, but does a best-first search which may not enumerate all possible results. It also filters out many punctuation and diacritic combinations which Tisane preserves.

## Development

For prerequisites, repository layout, building from source (Go, Svelte 5, and Bazel), cross-compilation, and running tests, see [DEVELOPMENT.md](DEVELOPMENT.md).

## AI Usage Disclaimer

Much of the code in this repository was written with AI-assisted tooling. User-facing documentation, such as this document, is primarily written by humans for humans.

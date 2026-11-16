# TEA Dictionary File Format Specification (`.tsd`)

This document specifies the binary structure, data structures, compression, indexing, and builder rules for **TEA (Crossword Helper)** dictionary files (`*.tsd`), as produced by `TSBuild.exe` (TEA Dictionary Builder 2.0) and consumed by `TEA.exe` and `TEACON.exe`.

---

## 1. Overview & Format Versions

A TEA dictionary file stores a compressed, length-bucketed word list optimized for fast pattern/anagram search without decompressing irrelevant word lengths, along with optional dictionary definitions, cross-reference aliases, annotations, qualifiers, and a full-text search (FTS) inverted index.

### Global Conventions
- **Byte Order**: All multi-byte integers (`uint16`, `int16`, `uint32`) are **little-endian**.
- **Character Encoding**: Raw 8-bit single-byte characters (typically Windows-1252 / ANSI for Western European dictionaries), interpreted through a 256-entry alphabet mapping (`.TSM`) table.
- **Maximum Word Length**: Up to **100** alphabetic letters (`L` in `1..100`, so `num_lengths <= 101`).

### Format Versions (`TSD0` vs. `TSD1`)
Both `TEA.exe` and `TEACON.exe` accept two magic signatures:

| Feature | `TSD0` (TEA 1.0 Legacy) | `TSD1` (TEA 2.0 / `TSBuild.exe`) |
| :--- | :--- | :--- |
| Magic bytes (`0x00..0x03`) | `"TSD0"` (`54 53 44 30`) | `"TSD1"` (`54 53 44 31`) |
| Emitted by `TSBuild.exe` 2.0 | No (legacy dictionaries only) | **Yes** (always) |
| Description field size | `80` bytes (NUL-padded) | `1000` bytes (NUL-padded) |
| Definitions (`def_pass_offsets`) | Absent | Present (`3 * uint32`) |
| Per-length FTS & link offset arrays | Absent | Present (`3 * num_lengths * uint32`) |
| Word bitstream per-word link bit | Absent | **1 raw bit** after each Sub-stream 1 word |
| Word bitstream Sub-stream 2 (FTS words) | Absent | Present (terminated by extra `\0`) |

Unless noted otherwise, all sections below describe **`TSD1`**, calling out `TSD0` differences where applicable.

---

## 2. Input Formats & Preprocessing Rules

Understanding how `TSBuild.exe` preprocesses its inputs is required to populate the `.tsd` header tables, word streams, definitions, and FTS indexes accurately.

### 2.1 Alphabet Mapping (`.TSM`) Files
A `.TSM` file defines which raw byte values (`0x00..0xFF`) are treated as alphabetic letters and how case/accent variants fold into equivalence classes.

1. **Initialization**: Start with an identity array `temp[b] = b` for `b = 0..255`.
2. **Line Parsing**: Read lines in text mode (`\r\n` normalized to `\n`, `\n` stripped). Every non-empty line must be 2 or 3 bytes with `'='` (`0x3D`) as the second byte:
   - `x=` (2 bytes): sets `temp[x] = 0` (`x` is **non-alphabetic** / punctuation — preserved in display words, ignored when counting word length and matching patterns).
   - `x=y` (3 bytes): sets `temp[x] = y` (`x` maps to `y`, e.g. `a=A`, `à=A`).
3. **Transitive Closure & Cycle Breaking**: For each `c = 0..255`, follow `a = temp[a]` starting at `a = c` until either `a == 0` or `a` has already been visited in this walk; set `tsm[c] = a`.
   - Any byte `k != 0` satisfying `tsm[k] == k` is the **canonical representative** of an alphabetic equivalence class (in `Default.TSM`, uppercase ASCII `'A'..'Z'` plus 37 uppercase extended ANSI letters, totaling 63 classes).
4. **Mandatory Non-Alphabetic Overrides**: Regardless of file contents, `TSBuild.exe` forces:
   ```c
   tsm[0x00] = 0;  // NUL
   tsm[0x0A] = 0;  // LF ('\n', used internally as annotation separator)
   tsm[0x0D] = 0;  // CR ('\r')
   ```

### 2.2 Word List Source (`.txt`) Grammar
Input files are read line-by-line in text mode (`\r\n` -> `\n`).
- **Comment lines**: Any line whose first byte is an unescaped `|` (`0x7C`) is ignored as a comment (though it still increments the 0-based input `line_index` counter).
- **Entry lines**:
  ```text
  [headword_item[:headword_item...]][;alias_item[:alias_item...]][|definition]
  ```
  Each `headword_item` or `alias_item` has the form:
  ```text
  word[+annotation[+qualifier]]
  ```
  - **Escapes (before `|` only)**: Backslash `\c` escapes the next byte `c` (so `\:` `:`, `\;` `;`, `\|` `|`, `\+` `+`, `\\` `\` are treated as literal characters rather than delimiters). Inside `definition` (after the first unescaped `|`), backslashes are **not** unescaped by the line parser because definitions may contain RTF (`{\rtf1...}`).
  - **Token length cap**: For any item, let `end` be the position of the second unescaped `+` (if present) or the item delimiter (`:`, `;`, `|`, `\n`). If `end - item_start > 2038` (`0x7F6`), the item is silently skipped.
  - **Alphabetic word length (`L`)**: Count of unescaped bytes `b` in `word` (strictly **before** the first unescaped `+`) with `tsm[b] != 0`. Items with `L < min_len` or `L > max_len` (where `1 <= min_len <= max_len <= 100`) are ignored.
  - **Display word (`display_word`)**:
    - Unescape `word`. If a first unescaped `+` is present **and** there is at least one byte between it and `end` (i.e. non-empty annotation), append `\n` (`0x0A`) followed by the unescaped `annotation`. (An empty annotation such as `bear++ n.` produces `"bear"` with **no** `\n`.)
    - Strip all trailing ASCII spaces (`0x20`), then strip all leading ASCII spaces (`0x20`).
  - **Qualifier (`qualifier`)**: Only meaningful on **headwords** (before `;`) when a `|definition` is present. Extracted by unescaping bytes after the second unescaped `+` up to the item delimiter (`:`, `;`, `|`). **Leading and trailing spaces are NOT stripped from qualifiers** (e.g., `bear++ n.|...` stores qualifier `" n."`).Qualifiers are stored only in Definition records, never in the word stream.
  - **Definition classification**:
    - **No definition (`pass = 0`)**: No unescaped `|` on the line, or zero valid items (`min_len <= L <= max_len`) preceded `|`.
    - **Pass 1 — Alias-only definition (`pass = 1`)**: `;` appeared before any valid headword (0 valid headwords before `;`, `>= 1` valid alias after `;`) followed by `|definition`. Used for cross-reference stubs (e.g. `;colour|See color`).
    - **Pass 2 — Headword definition (`pass = 2`)**: At least 1 valid headword appeared before `;` (or no `;` was present) followed by `|definition`.

### 2.3 Build Parameters & Defaults
`TSBuild.exe` exposes five profile settings (defaults shown):
- `min_len = 1` (`1..100`)
- `max_len = 100` (`1..100`)
- `max_fts_pct = 10` (`0..100`; `0` disables FTS occurrence lists; `1..100` discards FTS lists for words appearing in more than `max_fts_pct`% of input lines once list length exceeds 100)
- `sort_alphabetically = true` (if `false`, words within each length bucket are emitted in order of first appearance in the input file)
- `description = ""` (up to 999 bytes, NUL-padded to 1,000 bytes)

---

## 3. High-Level File Layout

### `TSD1` File Structure
```text
+-----------------------------------------------------------------------+ 0x0000
| Magic: "TSD1" (4 bytes)                                               |
| num_lengths = L_max + 1 (uint16)                                      |
| def_pass_offsets[3] (3 * uint32 = 12 bytes)                           |
| occ_index_offsets[num_lengths]   (num_lengths * uint32)               |
| def_link_offsets[num_lengths]    (num_lengths * uint32)               |
| def_link_counts[num_lengths]     (num_lengths * uint32)               |
| word_stream_offsets[num_lengths] (num_lengths * uint32)               |
| word_counts[num_lengths]         (num_lengths * uint32)               |
| description[1000]                (1000 bytes, NUL-padded)             |
| num_used_letters (int16), num_total_letters (int16)                   |
| char_table[256]                  (256 * 8 = 2048 bytes)               |
| num_calibration_rows (uint16)                                         |
| calibration_matrix[num_calibration_rows][num_used_letters] (int8)     |
+-----------------------------------------------------------------------+ def_pass_offsets[0]
| Pass 1 Definitions (alias-only, num_headwords == 0, deduplicated)     |
+-----------------------------------------------------------------------+ def_pass_offsets[1]
| Pass 2 Definitions (headword definitions, num_headwords >= 1)         |
+=======================================================================+ def_pass_offsets[2]
| Per-Length Section for each L in 1..L_max with word_counts[L] > 0:    |
|   1. FTS Occurrence Lists (dict words first, then FTS-only words)     |
|   2. FTS-Only Occurrence Pointer Table  <-- occ_index_offsets[L]      |
|   3. Huffman Word Bitstream (Sub-stream 1 + Sub-stream 2)             |
|                                         <-- word_stream_offsets[L]    |
|   4. Definition & Occurrence Link Table <-- def_link_offsets[L]       |
+=======================================================================+ EOF
```

For `TSD1`, the exact header size (which equals `def_pass_offsets[0]`) is:
$$\text{header\_size}_{\text{TSD1}} = 3072 + 20 \times \text{num\_lengths} + \frac{(\text{num\_lengths} - 1)\,\text{num\_lengths}}{2} \times \text{num\_used\_letters}$$

For legacy `TSD0`, `def_pass_offsets`, `occ_index_offsets`, `def_link_offsets`, and `def_link_counts` are omitted and `description` is 80 bytes. (Note: existing TEA 1.0 `TSD0` files also contain an unused legacy TEA 1.0 bigram table of $2 + (4\,\text{num\_lengths} - 3) \times \text{num\_used\_letters}^2$ bytes between `calibration_matrix` and `word_stream_offsets[1]`; `TEA.exe` 2.0 and `TEACON.exe` ignore it and seek directly via `word_stream_offsets[L]`.)

---

## 4. Header Specification

### 4.1 Header Fields (`TSD1`)

| Offset | Type | Name | Description |
| :--- | :--- | :--- | :--- |
| `0x00` | `char[4]` | `magic` | ASCII `"TSD1"` (`54 53 44 31`), or `"TSD0"` in legacy files. |
| `0x04` | `uint16` | `num_lengths` | `L_max + 1`, where `L_max` is the longest length with `word_counts[L] > 0` (`0` if dictionary has 0 words; max `101`). |
| `0x06` | `uint32[3]` | `def_pass_offsets` | *(TSD1 only)* `[0]` = file offset of Pass 1 definitions (= end of header); `[1]` = file offset of Pass 2 definitions; `[2]` = file offset of end of definitions. If there are no definitions, all three equal `header_size`. |
| `0x12` | `uint32[N]` | `occ_index_offsets` | *(TSD1 only, `N = num_lengths`)* File offset of the FTS-only (Sub-stream 2) occurrence pointer table for length `L`, or `0` if length `L` has no FTS-only words (or `word_counts[L] == 0`). |
| `0x12 + 4N` | `uint32[N]` | `def_link_offsets` | *(TSD1 only)* File offset of the Definition & Occurrence Link Table for dictionary words of length `L`, or `0` if `def_link_counts[L] == 0`. |
| `0x12 + 8N` | `uint32[N]` | `def_link_counts` | *(TSD1 only)* Total number of `uint32` entries at `def_link_offsets[L]` (`0` if none). |
| `0x12 + 12N` | `uint32[N]` | `word_stream_offsets` | File offset of the Huffman-compressed word bitstream for length `L`. Index `0` is always `0`. Empty slots `1 <= L < L_max` are **backward-filled** (see below). |
| `0x12 + 16N` | `uint32[N]` | `word_counts` | Number of unique dictionary words (Sub-stream 1) of length `L`. Index `0` is always `0`. |
| `0x12 + 20N` | `char[1000]` | `description` | NUL-terminated description string, padded with `0x00` bytes to 1,000 bytes (`TSBuild` copies at most 999 chars and writes `0x00` at byte 999). (`80` bytes in `TSD0`.) |
| `+1000` | `int16` | `num_used_letters` | Number of alphabetic equivalence classes with total frequency `> 0` (`0 <= num_used_letters <= num_total_letters < 254`). |
| `+1002` | `int16` | `num_total_letters` | Total number of alphabetic equivalence classes in `.TSM` (`1 <= num_total_letters < 254`; `63` for `Default.TSM`). |
| `+1004` | `CharEntry[256]` | `char_table` | 256 records of 8 bytes each (`2,048` bytes total) describing character mapping and Huffman codes (Section 4.3). |
| `+3052` | `uint16` | `num_cal_rows` | Number of rows in the calibration matrix: `(num_lengths - 1) * num_lengths / 2` (`0` if `num_lengths == 0`). |
| `+3054` | `int8[R][U]` | `calibration_matrix` | `R * U` signed bytes (`R = num_cal_rows`, `U = num_used_letters`) giving log-probability letter-position scores (Section 4.4). |

### 4.2 Backward-Filling of `word_stream_offsets`
`word_stream_offsets[0]` is always `0`. After writing all per-length sections, any empty length `1 <= L < L_max` (where `word_counts[L] == 0`, so `word_stream_offsets[L]` is still `0`) is filled backward from `L = L_max - 1` down to `1`:
```c
for (int L = num_lengths - 2; L >= 1; --L) {
    if (word_stream_offsets[L] == 0)
        word_stream_offsets[L] = word_stream_offsets[L + 1];
}
```
**Why this matters**: `TEA.exe` tests whether length `L < L_max` is non-empty in some search paths by checking `word_stream_offsets[L + 1] > word_stream_offsets[L]` (and checks `word_counts[L] > 0`). Leaving interior `word_stream_offsets[L] == 0` will break `TEA.exe` on sparse word lengths (such as `tsbuild1.tsd`, which has words of lengths 1, 6, 7, 8 and backward-fills `word_stream_offsets[2..5] = word_stream_offsets[6] = 4050`).

### 4.3 Character & Huffman Table (`char_table[256]`)
Each entry `char_table[b]` for byte value `b` (`0..255`) is an 8-byte little-endian structure:

```c
struct CharEntry {
    uint8_t  mapped_char;    // +0x00: tsm[b] (0 if non-alphabetic, else canonical byte)
    uint8_t  huff_code_len;  // +0x01: bit length of Huffman codeword (0 if unused, 1..32)
    int16_t  letter_index;   // +0x02: -1 (0xFFFF) if non-alphabetic, else 0..num_total_letters-1
    uint32_t huff_code;      // +0x04: MSB-first Huffman codeword in low huff_code_len bits
}; // 8 bytes
```

#### Assignment of `letter_index` (`0 .. num_total_letters - 1`)
Let `freq[b]` be the byte frequency accumulated during Pass 0 (Section 7.2), and for each canonical representative `c` (`tsm[c] == c && c != 0`), let `class_freq[c] = sum_{x: tsm[x] == c} freq[x]`.
1. **Used alphabetic classes** (`class_freq[c] > 0`): Assigned sequential indices `0 .. num_used_letters - 1` in ascending unsigned byte order of `c`.
2. **Unused alphabetic classes** (`class_freq[c] == 0`): Assigned sequential indices `num_used_letters .. num_total_letters - 1` in ascending unsigned byte order of `c`.
3. **Variant bytes** (`tsm[b] != b && tsm[b] != 0`): Inherit `letter_index[b] = letter_index[tsm[b]]`.
4. **Non-alphabetic bytes** (`tsm[b] == 0`): `letter_index[b] = -1` (`0xFFFF`).

*(Validation constraint: `TEA.exe` and `TEACON.exe` require `0 <= num_used_letters < 254`, `1 <= num_total_letters < 254`, and `num_used_letters <= num_total_letters`, rejecting the file otherwise.)*

#### Huffman Codeword Bit-Order & Fallback Rule
- **In the Header (`MSB-first`)**: `huff_code` is stored with the **root branch bit at bit `huff_code_len - 1`** and the **leaf branch bit at bit `0`**. Upon loading a `.tsd` file, `TEA.exe` and `TEACON.exe` walk `bit = huff_code_len - 1` down to `0` (`0` = left child, `1` = right child) to reconstruct the binary Huffman decode tree (and build a 256-entry 8-bit fast-lookup table). Any valid prefix-free binary code with `1 <= huff_code_len <= 32` is accepted by `TEA.exe` and `TEACON.exe`.
- **In the Word Bitstream (`LSB-first`)**: Immediately *after* writing the header (and *before* compressing word streams), `TSBuild.exe` bit-reverses `huff_code[b]` across its `huff_code_len[b]` bits so the root branch bit moves to bit `0`, then writes bits starting from bit `0` into 16-bit little-endian words (Section 6.3).
- **32-bit Overflow Fallback (Uncompressed Mode)**: If any symbol's Huffman tree depth exceeds `32` bits (`huff_code_len[b] > 32`), `TSBuild.exe` discards the tree and sets `huff_code_len[b] = 8` and `huff_code[b] = b` for **all** `b = 0..255`.

### 4.4 Letter-Position Calibration Matrix
`TEA.exe` and `TEACON.exe` use the calibration matrix to estimate which constrained pattern positions are most selective when planning search order.

- **Dimensions**: `num_cal_rows` rows by `num_used_letters` columns (`int8`), stored row-major.
- **Row Indexing**: Rows correspond to every `(L, pos)` pair for `L = 1 .. num_lengths - 1` and `pos = 0 .. L - 1` (`num_cal_rows = (num_lengths - 1) * num_lengths / 2`). Row start index for length `L` is `L * (L - 1) / 2`, so `(L, pos)` is row `L * (L - 1) / 2 + pos`.
- **Formula**: For a given length `L`, let $N_L = \text{word\_counts}[L]$ be the number of **unique dictionary words** (Sub-stream 1 only; annotations after `\n` and FTS-only words are excluded). For each `pos` in `0 .. L - 1` and `li` in `0 .. num_used_letters - 1`:
  - Let $c_{L,\text{pos},\text{li}}$ be the number of unique dictionary words of length `L` whose `pos`-th alphabetic character has `letter_index == li`.
  - Let $K_{L,\text{pos}} = |\{\text{li} : c_{L,\text{pos},\text{li}} > 0\}|$ be the number of distinct letters occurring at `(L, pos)`.
  - If $c_{L,\text{pos},\text{li}} == 0$ (including when $N_L == 0$): store `-128` (`0x80`).
  - Otherwise compute:
    $$x = \log_2\!\left(\frac{c_{L,\text{pos},\text{li}} \cdot K_{L,\text{pos}}}{N_L}\right)$$
    and round **half away from zero** (`int(x + 0.5)` if $x \ge 0$, `int(x - 0.5)` if $x < 0$), storing the signed 8-bit integer (`int8`).

---

## 5. Definitions Section (`TSD1` Only)

The Definitions section spans file offsets `def_pass_offsets[0]` through `def_pass_offsets[2]`. If no input lines have valid definitions, `def_pass_offsets[0] == def_pass_offsets[1] == def_pass_offsets[2] == header_size` (0 bytes).

### 5.1 Definition Record Layout
Every definition record has the following variable-length layout:

```text
+-------------------------------------------------------------------+
| num_headwords (uint8, plain UN-XORed byte, 0..255)                |
+-------------------------------------------------------------------+
| Repeated num_headwords times:                                     |
|   headword[i]   : bytes XOR 0xBD, terminated by 0xBD (0x00^0xBD)  |
|   qualifier[i]  : bytes XOR 0xBD, terminated by 0xBD (0x00^0xBD)  |
+-------------------------------------------------------------------+
| definition_text : bytes XOR 0xBD, terminated by 0xBD (0x00^0xBD)  |
+-------------------------------------------------------------------+
```

- **`0xBD` XOR Obfuscation**: Every string in the Definitions section (`headword[i]`, `qualifier[i]`, and `definition_text`) has each raw byte XORed with `0xBD` (`189`), followed by an XORed NUL terminator `0xBD` (`0x00 ^ 0xBD`). Because raw strings cannot contain `0x00`, `0xBD` never appears inside the XORed payload and unambiguously terminates each string.
- **Headword & Qualifier Extraction**:
  - Only valid headwords (`min_len <= L <= max_len` appearing **before** `;`) are included in `headword[i]` / `qualifier[i]`. If a line has more than 255 valid headwords, `num_headwords` is clamped to `255`.
  - `headword[i]` is the unescaped text **before** the first unescaped `+` (annotations between the first and second `+` are **not** included in `headword[i]`), trimmed of leading and trailing spaces (`0x20`).
  - `qualifier[i]` is the unescaped text **after** the second unescaped `+` (empty `""` -> single `0xBD` terminator if fewer than two `+` delimiters were present), **without** space trimming.
- **Definition Text**: Raw bytes after the first unescaped `|` up to the end of the line (with trailing `\n` stripped). May be plain text or Rich Text Format beginning with `{\rtf1` (which `TEA.exe` renders via a RichEdit control).

### 5.2 Pass 1 vs. Pass 2 & Pass 1 Deduplication
1. **Pass 1 (`def_pass_offsets[0] .. def_pass_offsets[1]`) — Alias-Only Definitions**:
   - Emitted for lines with 0 valid headwords before `;` and `>= 1` valid alias after `;` (`num_headwords` is always `0x00`, followed immediately by `definition_text ^ 0xBD` + `0xBD`).
   - **256-Entry Deduplication Cache**: Many generated word lists repeat identical cross-reference stubs (e.g., thousands of inflected forms pointing to `|See base word`). `TSBuild.exe` maintains an in-memory map of up to **256** `(definition_text -> file_offset)` entries during Pass 1. If an identical `definition_text` is already cached, no new record is written and the line reuses the cached `file_offset`; when inserting into a full 256-entry cache, the entry with the **smallest `file_offset`** is evicted.
   - Pass 1 definitions are **not** full-text indexed.
2. **Pass 2 (`def_pass_offsets[1] .. def_pass_offsets[2]`) — Headword Definitions**:
   - Emitted in input line order for all lines with `>= 1` valid headword and a `|definition` (`num_headwords >= 1`). Not deduplicated.

---

## 6. Per-Length Data Sections (`L = 1 .. L_max`)

After `def_pass_offsets[2]`, data is written sequentially for each word length `L` from `min_len` to `L_max` **strictly where `word_counts[L] > 0`**.

> **Important Builder/Parser Rule**: If `word_counts[L] == 0`, **nothing** is written to the file for length `L` — even if Pass 2 definitions contained `L`-letter FTS words! For any `L` with `word_counts[L] == 0`, `occ_index_offsets[L] = 0`, `def_link_offsets[L] = 0`, `def_link_counts[L] = 0`, and `word_stream_offsets[L]` is backward-filled from the next non-empty length.

For each length `L` with `word_counts[L] > 0`, up to four contiguous blocks are written in the exact order below:

### 6.1 Block 1: FTS Occurrence Lists (`TSD1` Only)
An **occurrence list** is a contiguous array of `k >= 1` `uint32` Pass 2 definition file offsets in ascending order, with **bit 31 (`0x80000000`) set on the final entry**:
```text
[ def_off_0, def_off_1, ..., (def_off_{k-1} | 0x80000000) ]
```

Occurrence lists for length `L` are written in two sub-passes:
1. **Dictionary Words (`sorted_words[L]`)**: For each unique dictionary word `w` of length `L` in emission order, compute its canonical key `canon_w` (the `L` bytes `tsm[b] != 0` from the word portion before `\n`). Look up `canon_w` in the `L`-letter FTS map (`MapB[L]`):
   - If found, **remove `canon_w` from `MapB[L]`** (so if multiple annotated variants like `word\nn.` and `word\nv.` share `canon_w`, only the **first** emitted variant receives the FTS link, and `canon_w` will not be emitted again in Sub-stream 2).
   - Apply the frequency cutoff check: if the list was already discarded during Pass 0, or if `list.size() > 100` **and** `list.size() * (100.0 / max_fts_pct) > total_input_lines`, set `w`'s `occ_ptr = 0x7FFFFFFF` (sentinel meaning *"too many occurrences to index"*) and write nothing.
   - Otherwise, record `w`'s `occ_ptr = current_file_offset` and write the `uint32` occurrence list (setting `0x80000000` on the last element).
2. **Remaining FTS-Only Words (`MapB[L]`)**: Iterate the remaining entries of `MapB[L]` in ascending unsigned byte order (`std::string` lexicographical order of `canon_w`). For each entry:
   - If discarded by the frequency cutoff (or `max_fts_pct == 0`), record `fts_occ_ptr = 0x7FFFFFFF` and write nothing.
   - Otherwise, record `fts_occ_ptr = current_file_offset` and write its `uint32` occurrence list (setting `0x80000000` on the last element).

### 6.2 Block 2: FTS-Only Occurrence Pointer Table (`occ_index_offsets[L]`, `TSD1` Only)
- If `MapB[L]` had `M >= 1` remaining FTS-only words after removing dictionary words:
  - Set `occ_index_offsets[L] = current_file_offset`.
  - Write `M` `uint32` values (`fts_occ_ptr[0 .. M-1]`), where each value is either the file offset of that word's occurrence list in Block 1 or `0x7FFFFFFF` (frequency cutoff sentinel). Note that bit 31 is **not** set on entries in `occ_index_offsets[L]`.
- If `M == 0`: leave `occ_index_offsets[L] = 0` and write 0 bytes.

### 6.3 Block 3: Huffman-Compressed Word Bitstream (`word_stream_offsets[L]`)
Set `word_stream_offsets[L] = current_file_offset`.

#### 16-Bit Little-Endian LSB-First Bit Packing
Bits are packed into a 16-bit unsigned accumulator (`uint16_t acc = 0`, `bit_pos = 0`):
- Writing a bit `b` (`0` or `1`) sets `acc |= (b << bit_pos)` and increments `bit_pos`.
- Whenever `bit_pos == 16`, `acc` is written as a 2-byte little-endian `uint16` (`acc & 0xFF`, `acc >> 8`), and `acc = 0, bit_pos = 0`.
- At the end of the word stream for length `L`, if `bit_pos > 0`, `acc` is flushed as a 2-byte little-endian `uint16` (leaving unused high bits `bit_pos .. 15` as `0`). Thus every word stream is a multiple of **2 bytes** in size.
- Each character byte `c` is encoded by emitting the `huff_code_len[c]` bits of the **bit-reversed** `huff_code[c]` (so bit `huff_code_len[c] - 1` of the header's `huff_code[c]` is emitted first, down to bit `0` of the header's `huff_code[c]`).

#### Dual-Substream Structure
1. **Sub-stream 1 — Dictionary Words (`word_counts[L]` entries)**:
   - For each unique dictionary word `i = 0 .. word_counts[L] - 1`:
     - Emit Huffman codes for each byte of `display_word[i]` (`word` or `word\nannotation`).
     - Emit the Huffman code for NUL (`0x00`).
     - **(`TSD1` only)** Emit **1 raw (un-Huffman-encoded) bit** `has_links`: `1` if this word has any direct definitions or an FTS `occ_ptr != 0`, else `0`. *(Omitted in `TSD0`.)*
   - After all `word_counts[L]` words, emit one extra Huffman code for NUL (`0x00`) representing an empty string terminator (with **no** trailing `has_links` bit).
   - *(In `TSD0`, flush the 16-bit bit buffer here; the stream ends.)*
2. **Sub-stream 2 — FTS-Only Words (`TSD1` only, `M` entries)**:
   - For each remaining FTS-only word `canon_w` in `MapB[L]` (in the exact same ascending `canon_w` order as `occ_index_offsets[L]`):
     - Convert each canonical byte `c` of `canon_w` to `most_freq_char[c]`, where `most_freq_char[c]` is the raw byte `x` in `c`'s equivalence class (`letter_index[x] == letter_index[c]`) that had the **strictly highest `freq[x]`** (ties broken by smallest byte value `x`, e.g., `'A'` -> `'a'` in lowercase word lists).
     - Emit Huffman codes for each converted byte, followed by the Huffman code for NUL (`0x00`).
     - **No** 1-bit flag is emitted after Sub-stream 2 words.
   - Emit one final Huffman code for NUL (`0x00`) (even when `M == 0`, so an empty Sub-stream 2 is a single Huffman-encoded `0x00`), then flush the 16-bit bit buffer.

### 6.4 Block 4: Definition & Occurrence Link Table (`def_link_offsets[L]`, `TSD1` Only)
Only Sub-stream 1 dictionary words whose 1-bit `has_links` flag was `1` have entries in this table, in the exact order those words appeared in Sub-stream 1.

- For each dictionary word `w` with `has_links == 1`, build its `uint32` link group:
  1. **Direct Definition Offsets**: Append the file offset (`def_pass_offsets[0] <= off < def_pass_offsets[2]`) of each Pass 1 or Pass 2 definition line on which `w` (`display_word`) appeared as a headword or alias, in input line order, **suppressing consecutive duplicate offsets**.
  2. **Trailing FTS Occurrence Pointer**: If `w` also had `occ_ptr != 0` from Block 1 (either an occurrence-list offset `>= def_pass_offsets[2]` or `0x7FFFFFFF`), append `occ_ptr` as the **last** element of `w`'s group.
  3. **End-of-Group Marker**: Bitwise-OR `0x80000000` onto the **final** `uint32` of `w`'s group.
- Concatenate all groups for length `L`:
  - If non-empty: set `def_link_offsets[L] = current_file_offset`, set `def_link_counts[L]` to the total number of `uint32` integers across all groups for length `L`, and write the `uint32` array.
  - If empty (`0` words had `has_links == 1`): leave `def_link_offsets[L] = 0` and `def_link_counts[L] = 0`.

#### How `TEA.exe` Resolves a Link Group (`GetDefinitionOffsets`)
When the user views definitions for the `k`-th dictionary word with `has_links == 1` of length `L`:
1. `TEA.exe` scans `def_link_offsets[L]` (`def_link_counts[L]` entries), counting `0x80000000`-terminated groups until reaching group `k`.
2. Every non-final entry in group `k` is a direct definition offset (`< def_pass_offsets[2]`).
3. For the final entry `last_val = raw_last & 0x7FFFFFFF`:
   - If `def_pass_offsets[0] <= last_val < def_pass_offsets[2]`, `last_val` is also a direct definition offset.
   - Else if `last_val == 0x7FFFFFFF`, `TEA.exe` notes that full-text occurrences exceeded the indexing threshold.
   - Else (`last_val >= def_pass_offsets[2]`), `TEA.exe` seeks to `last_val` and reads `uint32` FTS occurrence offsets until encountering an entry with bit 31 (`0x80000000`) set.

---

## 7. Complete Builder Algorithm & Subtle Implementation Details

To build a tool that generates `.tsd` files (including 100% byte-identical output to `TSBuild.exe`), follow these steps and edge-case rules:

### 7.1 Word Sorting & Collation Quirk
When `sort_alphabetically == false`, unique `display_word` entries within each length `L` are ordered by the `line_index` of their **first** appearance in the `.txt` file.

When `sort_alphabetically == true`, `TSBuild.exe` orders `display_word` strings using a two-level comparator:
1. **Primary key**: Strip bytes where `SORT_TABLE[b] == 0` and compare the mapped byte sequences unsigned-lexicographically (`SORT_TABLE` folds `'a'..'z'` -> `'A'..'Z'`, Windows-1252 accented letters to base uppercase ASCII `'A'..'Z'` or `0x8C`/`0xC6`/`0xDE`/`0xDF`, and maps punctuation/digits/whitespace including `\n` to `0`).
   - *Subtle `TSBuild.exe` detail*: While `TEA.exe` does **not** depend on word sort order during pattern search (it scans each length's bitstream linearly and sorts matches for display), `TSBuild.exe` uses a fixed Windows-1252 collation table (matching `Default.TSM`) rather than the active `.TSM` when sorting `MapA`. Note also that if a word has an `+annotation`, the annotation's alphabetic letters participate in `SORT_TABLE` after the base word's letters.
2. **Tie-breaker**: Standard unsigned byte-by-byte lexicographical comparison of the full `display_word` (`memcmp` + length). Exact `display_word` duplicates are merged into a single word entry whose definition list concatenates their `line_index` references.

### 7.2 Exact Byte-Frequency Counting & FTS RTF Stripping (Pass 0)
`freq[0..255]` is accumulated during the initial scan of the `.txt` file **before** deduplicating words:
1. **Every valid headword or alias occurrence** (`min_len <= L <= max_len`, *including* duplicate occurrences across lines or on the same line):
   - Increment `freq[b]` for every byte `b` in its trimmed `display_word` (`word` or `word\nannotation`, so `freq[0x0A]` is incremented for annotated words).
   - Increment `freq[0x00]` by `1` (counting its NUL terminator).
2. **Pass 2 Definition FTS Tokenization** (executed for every line classified as Pass 2, even if `max_fts_pct == 0`):
   - **Plain text vs. RTF**: If the text after `|` does **not** start with `{\rtf1`, strip a trailing `\n` and scan raw bytes. If it **does** start with `{\rtf1`, strip RTF markup first:
     - Ignore `{` and `}`.
     - Handle `\` escapes: `\'XX` decodes two hex digits into one byte; `\-` and `\_` emit `'-'` (`0x2D`); `\*` is ignored; `\\`, `\{`, `\}` emit literal characters; `\controlword[-N][ ]` (ASCII letters, optional `-` and digits, optional single trailing space `0x20` consumed) emits a single space `' '` (`0x20`) if the control word is in `{bin, par, line, tab, emdash, endash, bullet, lquote, rquote, ldblquote, rdblquote}` (and `\binN` skips `N` subsequent bytes), otherwise emits nothing.
   - **Tokenizing plain definition bytes**: Scan bytes `b`, mapping `mb = tsm[b]`:
     - Whenever `mb != 0`:
       - **Zero-frequency promotion**: If `freq[mb] == 0`, set `freq[mb] = 1`! (This ensures any letter class appearing only inside definitions or only in lowercase in headwords still has a valid Huffman code and `num_used_letters` entry.)
       - Append `mb` to the current FTS token.
     - On `mb == 0` (or end of definition) with a non-empty token `tok`:
       - Require `tok.length() <= max_len` and check that `tok` is **not** equal to the canonical form of any valid headword or alias on this **same** line.
       - Insert/update `tok -> [line_index]` in `MapB[tok.length()]` (suppressing duplicate `line_index` within the same line; if `max_fts_pct == 0`, store `NULL` list; if `list.size() > 100 && list.size() * (100.0 / max_fts_pct) > line_index`, discard list and replace with `NULL` sentinel).

### 7.3 Building the Huffman Tree
1. Create a leaf `(sym = b, freq = freq[b], len = 0, code = 0)` for every `b` in `0..255` with `freq[b] > 0` in ascending `b` order. (If the input produced 0 words, `freq` is all zeros and `char_table` has `huff_code_len = 0` everywhere.)
2. Repeat `num_leaves - 1` times on the active array of `rem` nodes:
   - Sort active nodes in **descending** order of `freq`.
   - For every leaf under `nodes[rem - 2]` (higher or equal frequency of the two smallest), set `code |= (0 << len); len++`.
   - For every leaf under `nodes[rem - 1]` (smallest frequency), set `code |= (1 << len); len++`.
   - Replace `nodes[rem - 2]` with a parent node of `freq = nodes[rem - 2].freq + nodes[rem - 1].freq` and decrement `rem`.
3. Check if any `len > 32`; if so, replace with `len = 8, code = b` for all `b = 0..255`.
4. Write `code` and `len` into `char_table` in the header **as-is** (MSB-first), then bit-reverse each `code` across `len` bits before encoding the per-length word bitstreams.

*(Note: Any standard Huffman construction producing code lengths `<= 32` — including canonical Huffman codes — is 100% compatible with `TEA.exe`, `TEACON.exe`, and `TSMerge.exe`, because readers reconstruct the Huffman tree solely from `char_table` in the header.)*

### 7.4 Summary of Build Pipeline
1. **Load `.TSM`** and compute transitive closure + `0x00`/`0x0A`/`0x0D` overrides.
2. **Pass 0 (Scan `.txt`)**: Parse headwords/aliases into `MapA[min_len..max_len]`, classify each line (`0`, `1`, `2`), tokenize Pass 2 definitions into `MapB[1..max_len]`, and accumulate `freq[0..255]`.
3. **Build Alphabet & Huffman Tables**: Build Huffman codes from `freq`, assign `letter_index` (`num_used_letters`, `num_total_letters`), compute `most_freq_char[0..255]` for Sub-stream 2, deduplicate `MapA` per length to get `word_counts[L]` and `num_lengths = L_max + 1`, and compute `calibration_matrix`.
4. **Write Placeholder Header**: Write `"    "` (4 spaces) + `num_lengths` + zeroed offset arrays + `word_counts` + `description[1000]` + letter counts + `char_table[256]` + `calibration_matrix`. Record `def_pass_offsets[0] = ftell()`.
5. **Write Definitions (Pass 1 then Pass 2)**: Record `def_pass_offsets[1]` between passes and `def_pass_offsets[2]` at completion, storing the resulting `file_offset` for each defined input line.
6. **Bit-Reverse Huffman Codes** in memory for LSB-first bitstream emission.
7. **Write Per-Length Sections (`L = min_len .. L_max` where `word_counts[L] > 0`)**:
   - Emit Block 1 (FTS occurrence lists for dictionary words then FTS-only words), Block 2 (`occ_index_offsets[L]`), Block 3 (`word_stream_offsets[L]`: Sub-stream 1 with 1-bit `has_links` + `\0` + Sub-stream 2 + `\0` + 16-bit flush), and Block 4 (`def_link_offsets[L]`, `def_link_counts[L]`).
8. **Finalize Header**: Backward-fill zero `word_stream_offsets[1 .. L_max - 1]`, `rewind` to offset `0`, and write `"TSD1"`, `num_lengths`, `def_pass_offsets[3]`, `occ_index_offsets`, `def_link_offsets`, `def_link_counts`, `word_stream_offsets`, and `word_counts`.

---

## 8. Concrete Walkthrough: `tsbuild2.tsd`

`binary/tsbuild2.tsd` (`7,464` bytes) is generated from `binary/tsbuild2.txt` with `Default.TSM`, default settings (`min_len = 1`, `max_len = 100`, `max_fts_pct = 10`, `sort_alphabetically = true`), and description `"TEA&Sympathy Dictionary Builder Example 2"`.

### 8.1 Complete File Offset Map (`tsbuild2.tsd`)

| File Offset (Hex) | File Offset (Dec) | Size | Section | Details |
| :--- | :--- | :--- | :--- | :--- |
| `0x0000..0x0003` | `0..4` | `4` | `magic` | `"TSD1"` (`54 53 44 31`) |
| `0x0004..0x0005` | `4..6` | `2` | `num_lengths` | `13` (`L_max = 12`, `"Crossword Man"`) |
| `0x0006..0x0011` | `6..18` | `12` | `def_pass_offsets` | `[5360, 5421, 6570]` (`0x14F0, 0x152D, 0x19AA`) |
| `0x0012..0x0045` | `18..70` | `52` | `occ_index_offsets[13]` | `[0, 0, 0, 6682, 6852, 7024, 7146, 7222, 0, 7332, 7418, 0, 0]` |
| `0x0046..0x0079` | `70..122` | `52` | `def_link_offsets[13]` | `[0, 0, 0, 6740, 6944, 7074, 7206, 7272, 0, 0, 7438, 0, 7456]` |
| `0x007A..0x00AD` | `122..174` | `52` | `def_link_counts[13]` | `[0, 0, 0, 4, 8, 4, 1, 5, 0, 0, 1, 0, 2]` |
| `0x00AE..0x00E1` | `174..226` | `52` | `word_stream_offsets[13]` | `[0, 6714, 6714, 6714, 6888, 7044, 7170, 7234, 7340, 7340, 7422, 7446, 7446]` |
| `0x00E2..0x0115` | `226..278` | `52` | `word_counts[13]` | `[0, 0, 0, 2, 7, 3, 1, 5, 0, 1, 1, 0, 1]` (21 unique words total) |
| `0x0116..0x04FD` | `278..1278` | `1000` | `description` | `"TEA&Sympathy Dictionary Builder Example 2\0..."` |
| `0x04FE..0x0501` | `1278..1282` | `4` | Letter counts | `num_used_letters = 26`, `num_total_letters = 63` |
| `0x0502..0x0D01` | `1282..3330` | `2048` | `char_table[256]` | e.g. `'A'` (`65`): `(mapped=65, len=6, idx=0, code=53)`; `'a'` (`97`): `(mapped=65, len=4, idx=0, code=12)` |
| `0x0D02..0x0D03` | `3330..3332` | `2` | `num_cal_rows` | `78` (`12 * 13 / 2`) |
| `0x0D04..0x14EF` | `3332..5360` | `2028` | `calibration_matrix` | `78 * 26` `int8` bytes |
| `0x14F0..0x152C` | `5360..5421` | `61` | Pass 1 Definitions | 1 alias-only record (`;alias|...`, `num_headwords = 0`) |
| `0x152D..0x19A9` | `5421..6570` | `1149` | Pass 2 Definitions | 15 headword definition records (`num_headwords` in `1..2`) |
| `0x19AA..0x1D27` | `6570..7464` | `894` | Per-Length Sections | Emitted for `L = 3, 4, 5, 6, 7, 9, 10, 12` (`L = 1, 2, 8, 11` skipped since `word_counts[L] == 0`) |

### 8.2 Walkthrough of `L = 3` (`6570..6756`, 186 bytes) and `L = 12` Hybrid Link Group
- **`L = 3` (`0x19AA..0x1A63`)**:
  - `tsbuild2.txt` has 2 unique 3-letter dictionary words (`"aid"` and `"tap"`, neither occurring inside other lines' definitions) and 8 FTS-only 3-letter words (`"and"`, `"for"`, `"man"`, `"one"`, `"see"`, `"tea"`, `"url"`, `"www"`).
  - **`6570..6682` (112 bytes)**: 8 `0x80000000`-terminated occurrence lists (28 `uint32`s total) for the 8 FTS-only words.
  - **`6682..6714` (32 bytes, `occ_index_offsets[3] = 6682`)**: 8 `uint32` offsets `[6570, 6574, 6610, 6618, 6658, 6666, 6674, 6678]`.
  - **`6714..6740` (26 bytes, `word_stream_offsets[3] = 6714`)**: 13 `uint16` words encoding Sub-stream 1 (`"aid\0"` + bit `1`, `"tap\0"` + bit `1`, `"\0"`) followed by Sub-stream 2 (`"and\0"`, `"for\0"`, `"man\0"`, `"one\0"`, `"see\0"`, `"tea\0"`, `"url\0"`, `"www\0"`, `"\0"`), zero-padded to 16 bits.
  - **`6740..6756` (16 bytes, `def_link_offsets[3] = 6740`, `def_link_counts[3] = 4`)**:
    - Group 0 (`"aid"`): `[0x800015AA]` -> direct definition at `5546` (`0x15AA`).
    - Group 1 (`"tap"`): `[0x0000172C, 0x00001782, 0x800017D9]` -> three direct definitions (`tap 1 v.` at `5932`, `tap 2 v.` at `6018`, `tap 3 n.` at `6105`).
- **`L = 12` (`"Crossword Man"`, `def_link_offsets[12] = 7456`, `def_link_counts[12] = 2`)**:
  - `"Crossword Man"` has a direct headword definition at offset `6394` (`0x18FA`, inside `[5360, 6570)`) **and** its canonical form `CROSSWORDMAN` appears inside `"TEA Support"`'s definition (`support@crosswordman.com` at offset `6478` = `0x194E`), producing a 1-element occurrence list `[0x8000194E]` at file offset `7442` (`0x1D12`).
  - Its link group at `7456` is `[0x000018FA, 0x80001D12]`: `TEA.exe` sees `0x18FA < 6570` (direct definition) and `(0x80001D12 & 0x7FFFFFFF) = 7442 >= 6570` (indirect FTS occurrence list pointer -> dereferences `7442` to obtain `6478`).

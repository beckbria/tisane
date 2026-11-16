# TEA Pattern Search Language Specification

This document specifies the query syntax, matching semantics, configuration options, validation rules, and result formatting of the **TEA (Crossword Helper)** pattern search language in complete detail sufficient to implement a 100%-compatible search engine.

For the binary dictionary file format, see [TEA_Dictionary_Format.md](TEA_Dictionary_Format.md).

---

## 1. Overview & Data Model

A TEA query is a **boolean expression** combining one or more **atomic patterns**. Each atomic pattern (and each recursive sub-pattern) consists of an optional **Template**, an optional **Anagram** introduced by `;`, or **both** simultaneously.

### 1.1 Candidate Entry Representation

Every dictionary entry evaluated by the engine consists of:
1. **Display String**: A sequence of raw 8-bit characters as stored in the dictionary (e.g., `"don't"`, `"café"`, `"New York"`). An entry may also carry an inline annotation (delimited by `0x0A` in `.tsd` streams); annotations are strictly metadata and never participate in pattern matching.
2. **Character Classification & Canonical Mapping**: Using the active dictionary's alphabet mapping table (`.tsm` / `.tsd`), each byte `c` (`0x00..0xFF`) is classified as either:
   - **Alphabetic**: Mapped to a canonical letter equivalence class `letter_idx` (`0 .. num_used_letters - 1`). By default, uppercase/lowercase variants (`A` and `a`) and accented variants (`é`, `è`, `ê`, and `e`) map to the **same** canonical letter.
   - **Non-Alphabetic (Punctuation)**: Spaces (`' '`), hyphens (`'-'`), apostrophes (`'\''`), slashes (`'/'`), digits, and other symbols not mapped to an equivalence class.
3. **Canonical Word Length ($L$)**: The number of **alphabetic** characters in the display string (`1 <= L <= 100`). Non-alphabetic characters do not contribute to $L$.

By default, patterns match against the **canonical letter sequence** of length $L$, ignoring case, accents, and punctuation (`dont` matches `don't`; `cafe` matches `café`; `icecream` matches `ice cream` and `ice-cream`), unless explicitly overridden with escapes (`\`) or template punctuation.

### 1.2 Engine Configuration Parameters

A compatible engine exposes the following query-evaluation settings:

| Parameter | GUI Default (`TEA.exe`) | CLI Flag & Default (`TEACON.exe`) | Effect |
| :--- | :--- | :--- | :--- |
| **At-Sign Macro (`@`)** | `[aeiou]` | `/A:<str>` (`[aeiou]`) | Pre-parser expansion for unescaped `@`. |
| **Hash Macro (`#`)** | `[^aeiou]` | `/H:<str>` (`[^aeiou]`) | Pre-parser expansion for unescaped `#`. |
| **Exclusive Variables** | `false` (off) | `/X:0` (`false`); `/X:1` (`true`) | When `true`, distinct variable digits `0`–`9` in an atomic pattern must bind to distinct canonical letters. |
| **Ignore Unused Punctuation** | `true` (on) | `/U:0` (`false`); `/U:1` (`true`) | Controls whether dictionary punctuation characters *not* mentioned in a template are ignored or rejected when punctuation matching is active. |
| **Length Bounds** | `[1, 100]` | `[shortest, longest]` (`1..100`) | Restricts candidate canonical word lengths $L$ considered during Word Search. |
| **Search Mode** | Word Search | Word Search (or FTS in GUI) | Evaluates boolean expressions per headword (**Word Search**) or across tokenized definition records (**Full Text Search**). |
| **Show Balances** | Optional | Optional | Appends `+added` / `-omitted` canonical letter balances to anagram matches. |

---

## 2. Formal Grammar

Macro symbols `@` and `#` are **not** grammar tokens; they undergo textual expansion *before* parsing (Section 3). After macro expansion, valid queries conform to the following EBNF grammar:

```ebnf
Query          ::= Expr
Expr           ::= UnaryExpr ( ( '&' | '|' ) UnaryExpr )*
UnaryExpr      ::= '!'? ( '(' Expr ')' | AtomicPattern )

AtomicPattern  ::= Template ( ';' Anagram )?
                 | ';' Anagram

Template       ::= TemplateItem+
TemplateItem   ::= Letter
                 | '.' | '?'                 (* single-letter match-all *)
                 | '*'                       (* 0..N letter sequence wildcard *)
                 | ChoiceList                (* [abc] or [^abc] *)
                 | Digit                     (* '0'..'9' letter variable *)
                 | '(' AtomicPattern ')'     (* embedded sub-pattern *)
                 | '\' AnyChar               (* escaped literal / case-exact letter *)
                 | PunctChar                 (* space, hyphen, apostrophe, etc. *)

Anagram        ::= AnagramItem+
AnagramItem    ::= Letter
                 | '.' | '?'                 (* 1 arbitrary extra letter in pool *)
                 | '*'                       (* 0..N arbitrary extra letters in pool *)
                 | ChoiceList                (* 1 letter chosen from [abc] / [^abc] *)
                 | Digit                     (* '0'..'9' letter variable in pool *)
                 | '(' AtomicPattern ')'     (* contiguous span inside anagram *)
                 | ' '                       (* ignored in anagrams *)
```

---

## 3. Pre-Parser Macro Expansion (`@` and `#`)

Before splitting boolean operators or parsing patterns, the engine scans the raw query string left-to-right in a single pass and expands unescaped `@` and `#`:

1. **Single-Pass (Non-Recursive)**: Replacement text is appended directly to the expanded query buffer without re-scanning it for `@` or `#` (even if `@`'s replacement contains `#` or vice versa).
2. **Empty Macro Definition**: If a macro's configured replacement string is empty (`""`), the literal `@` or `#` character is left unchanged in the query string.
3. **Backslash Escaping**: Whenever `\` is encountered and at least one character follows it, **both** `\` and the immediately following byte are copied verbatim without macro expansion. For example, `\@` remains `\@` after macro expansion, so the template parser subsequently treats `@` as an escaped literal punctuation character.
4. **Context-Free Textual Substitution**: Expansion occurs blindly regardless of surrounding delimiters:
   - `@#@#` with defaults expands to `[aeiou][^aeiou][aeiou][^aeiou]`.
   - Writing `[@y]` with the default `@ = [aeiou]` expands to `[[aeiou]y]`, which fails validation with error `2` because `[` is illegal inside a choice list `[...]`.

---

## 4. Logical Expressions & Parenthesis Disambiguation

### 4.1 Boolean Operators & Precedence

Multiple atomic patterns can be combined using boolean operators:
- **`&` (AND)**: Both operands must match.
- **`|` (OR)**: Either operand must match.
- **`!` (NOT)**: Unary prefix negation (`!*ing` matches words not ending in `ing`; `!(a* | b*)` negates a parenthesized expression).
- **Precedence & Associativity**: `&` and `|` have **equal precedence** and evaluate strictly **left-to-right** with short-circuiting, unless explicitly grouped with `(...)` (supported up to **32** logical nesting levels).
  - Example: `a* | b* & *s` evaluates as `(a* | b*) & *s`, **not** `a* | (b* & *s)`.

### 4.2 Whitespace Rules Around Operators

Whitespace handling depends on syntactic position:
- **Around `&` and `|` operands**: Unescaped leading and trailing spaces (`0x20`) around each operand segment split by `&` or `|` are stripped (`p* & *g` is identical to `p*&*g`).
- **Internal template spaces**: Spaces *between* tokens inside a template are preserved as literal space punctuation (`... ....` matches a 3-letter word, a space, and a 4-letter word). To match a leading or trailing space at the start or end of an operand, escape it as `\ `.
- **Inside `;anagram`**: Spaces (`0x20`) are unconditionally ignored (`;new york` is identical to `;newyork`).

### 4.3 Disambiguating Logical `(...)` vs. Sub-Pattern `(...)`

Because parentheses `(` and `)` denote **both** boolean grouping (`(a* | b*) & *s`) and embedded sub-patterns (`(;orange)*`), a compatible parser must disambiguate them using TEA's token-segment algorithm:

1. **Top-Level Operator Split**: Scan the macro-expanded query left-to-right (skipping any byte immediately following `\`, and ignoring characters inside `[...]`) to split on unescaped `&` and `|` operators into a sequence of operand segments. (Note: unescaped `&` and `|` are never legal inside an `AtomicPattern` or sub-pattern.) Trim leading and trailing unescaped spaces on each segment.
2. **Per-Segment Parenthesis Counting**: On each operand segment, scan unescaped characters (outside `[...]`) and record:
   - `open_cnt`: total unescaped `(` in the segment.
   - `close_cnt`: total unescaped `)` in the segment.
   - `open_at_bang`: number of unescaped `(` seen prior to an unescaped `!` prefix (if `!` is present).
3. **Balanced Sub-Pattern Retention**:
   - Compute `common = min(open_cnt, close_cnt)`.
   - If `!` is present and `open_cnt - common < open_at_bang`, clamp `common = open_cnt - open_at_bang` so that `!` remains outside any sub-pattern parentheses (ensuring `!(a*)` and `!((;cat)*)` treat `!` as logical negation rather than part of the template).
   - Set `logical_opens = open_cnt - common` and `logical_closes = close_cnt - common`.
4. **Strip Outer Logical Parentheses**:
   - Strip `logical_opens` leading `(` tokens (along with any `!` attached either before a logical `(` as `!(...)` or directly before the atomic pattern as `!pattern`) from the left of the segment as **logical grouping opens**.
   - Strip `logical_closes` trailing `)` tokens from the right of the segment as **logical grouping closes**.
   - All remaining balanced parentheses (`common` pairs) stay inside the `AtomicPattern` and are parsed as template or anagram sub-patterns.
   - Verify that across the entire expression, total `logical_opens == logical_closes` and nesting depth never drops below `0` or exceeds `32` (raising error `7` or `8` on mismatch).

### 4.4 Word Search vs. Full Text Search (FTS) Semantics

- **Word Search Mode**: The boolean expression is evaluated against each candidate **dictionary headword** individually (`p* & *y` matches single headwords starting with `p` and ending with `y`).
- **Full Text Search (FTS) Mode**: Each leaf `AtomicPattern` tests individual tokenized words (`1..100` contiguous alphabetic characters, plus associated headwords) inside a **definition record**, while `&`, `|`, and `!` evaluate **co-occurrence across the entire definition** (`eagle & America & !bald` matches headwords whose definition contains at least one word matching `eagle`, at least one word matching `America`, and zero words matching `bald`).

---

## 5. Templates, Anagrams, and Hybrid Patterns

An `AtomicPattern` takes one of three forms:

| Pattern Form | Syntax | Semantics | Examples |
| :--- | :--- | :--- | :--- |
| **Template only** | `<template>` | Matches words whose canonical letter sequence (and active punctuation) positionally matches `<template>`. | `c.t` → `cat`, `cot`, `cut`<br>`*ing` → words ending in `ing` |
| **Anagram only** | `;<anagram>` | Matches words whose canonical letters can be formed by rearranging `<anagram>`. Spaces inside `<anagram>` are ignored. | `;listen` → `enlist`, `inlets`, `listen`, `silent`, `tinsel` |
| **Hybrid (`Template;Anagram`)** | `<template>;<anagram>` | Candidate word must satisfy **both** constraints simultaneously: its positional structure matches `<template>` **and** its canonical letter multiset satisfies `<anagram>` under the hybrid length/omission rules below. | `e........;cats` → 9-letter words starting `e` containing `c,a,t,s` (`escalator +AELOR`)<br>`.....*;aadmort` → 5-to-7-letter rack words from `aadmort` (`aorta -DM`, `matador`) |

At most **one** unescaped `;` may appear inside any single atomic pattern or sub-pattern; a second unescaped `;` triggers validation error `6` (though nested `(sub-patterns)` each have their own independent `;` allowance).

### 5.1 Hybrid Length & Omission Budget Rules

Let:
- $[T_{\min}, T_{\max}]$ be the matched canonical length bounds of `<template>` ($T_{\max} = \infty$ if `<template>` contains `*` or a variable-length sub-pattern).
- $A_{\text{spec}}$ be the total minimum letter count of explicit items in `<anagram>` (individual letters, choice lists, letter variables, plus the minimum lengths of any embedded sub-patterns).
- $A_{\text{dots}}$ be the count of single-letter wildcards (`.` or `?`) in `<anagram>`.
- $A_{\min} = A_{\text{spec}} + A_{\text{dots}}$.
- `anag_unbounded` be `true` if `<anagram>` contains `*` (or a sub-pattern of unbounded/variable length exceeding fixed bounds), and `false` otherwise.

Candidate canonical word lengths $[L_{\min}, L_{\max}]$ (subsequently intersected with `[shortest, longest]` in `1..100`) and letter omission rules are determined as follows:

1. **Pure Anagram (`;<anagram>`, empty template)**:
   - Length bounds: $[A_{\min}, \infty)$ if `anag_unbounded` is `true`, or $[A_{\min}, A_{\min}]$ otherwise.
   - **Zero** specified items may be omitted: every letter, choice list, variable, and sub-pattern in `<anagram>` must be used in the candidate word. Any extra letters up to $L$ are supplied by `.` or `*` (reported as `+<added>`).

2. **Hybrid (`<template>;<anagram>`, non-empty template)**:
   - **Length Bounds & Sub-Anagram Cap**: Candidate word lengths start from $[T_{\min}, T_{\max}]$. However, if the template is variable-length ($T_{\max} > T_{\min}$), `anag_unbounded` is `false` (no `*` in `<anagram>`), and $T_{\min} < A_{\min}$, then $L_{\max}$ is **capped to $A_{\min}$**:
     $$L_{\min} = T_{\min}, \qquad L_{\max} = \begin{cases} A_{\min} & \text{if } T_{\max} > T_{\min} \text{ and } \neg\text{anag\_unbounded} \text{ and } T_{\min} < A_{\min} \\ T_{\max} & \text{otherwise} \end{cases}$$
     - **Rack Sub-Anagrams**: `*;defijnx` ($T=[0,\infty), A_{\min}=7$) caps candidate lengths to $[1, 7]$; `.....*;aadmort` ($T=[5,\infty), A_{\min}=7$) caps lengths to $[5, 7]$; `.....;defijnx` ($T=[5,5]$) fixes length to $5$.
     - **Template-Forced Super-Anagrams**: `e........;cats` ($T=[9,9], A_{\min}=4$) has fixed template length ($T_{\max} = T_{\min} = 9$), so $L = 9 > A_{\min}$, requiring all of `c, a, t, s` plus $5$ unspecified letters (e.g., `escalator +AELOR`).
   - **Exact Omission Budget**: For a candidate word of length $L$, the maximum number of explicit anagram letters/items ($A_{\text{spec}}$) that may be **omitted** (left unused) is:
     $$\text{max\_omissions}(L) = \max\bigl(0,\; (A_{\text{spec}} + A_{\text{dots}}) - L\bigr)$$
     Equivalently, initialize `budget = min(0, L - A_min)` and add `1` to `budget` for every specified anagram item not matched in the candidate word; the anagram match succeeds if and only if `budget <= 0` (and every embedded sub-pattern/variable unification succeeds).
     - **Worked Example (`......*;ehiktt.`)**: Here $A_{\text{spec}} = 6$ (`e,h,i,k,t,t`), $A_{\text{dots}} = 1$ (`.`), so $A_{\min} = 7$ and $L \in [6, 7]$:
       - At $L = 6$: $\text{max\_omissions}(6) = \max(0, 7 - 6) = 1$. Up to $1$ rack letter may be omitted while the `.` acts as $1$ blank tile (matching `hitter` with balance `-K+R`).
       - At $L = 7$: $\text{max\_omissions}(7) = \max(0, 7 - 7) = 0$. All $6$ specified letters must appear plus $1$ blank letter (matching `thicket` with balance `+C`).

---

## 6. Pattern Elements

### 6.1 Single-Letter Wildcards (`.` and `?`)
`.` and `?` are 100% synonymous everywhere:
- **In a Template**: Matches exactly **one** canonical letter at that template position. Never matches punctuation.
- **In an Anagram**: Supplies **one** arbitrary canonical letter ("blank tile") to the anagram pool (`;pear.` matches 5-letter words containing `p, e, a, r` plus one extra letter).

### 6.2 Multi-Letter Wildcard (`*`)
Matches **zero or more** (`0..N`) canonical letters:
- **In a Template**: Matches any contiguous sequence of `0` or more canonical letters (`p*t` matches `pt`, `pat`, `plant`).
- **In an Anagram**:
  - **Super-anagrams (`;letters*` or `;*letters`)**: Allows `0..N` extra arbitrary letters beyond the specified items (`;post*` matches words of length $\ge 4$ containing at least `p, o, s, t`, such as `post`, `stop`, `sports +RS`, `postmaster +AMERT`). Placement of `*` within `<anagram>` has no effect (`;*post` ≡ `;post*`).
  - **Sub-anagrams (`*;letters` or `.....*;letters`)**: Placing `*` in `<template>` with an `*`-free `<anagram>` activates the hybrid length cap and omission budget (Section 5.1).

### 6.3 Choice Lists (`[...]` and `[^...]`)
Matches a single canonical letter drawn from a set:
- **`[abc]`**: Matches one letter whose canonical equivalence class belongs to $\{a, b, c\}$ (because characters fold through the dictionary's `.tsm` map, `[e]` matches `e`, `E`, `é`, `è`, `ê`).
- **`[^abc]`**: Negated choice list; matches any active canonical letter in the dictionary **except** $\{a, b, c\}$.
- **In Templates vs. Anagrams**:
  - In a template, tests the candidate letter at that single position (`[bc]at` → `bat`, `cat`).
  - In an anagram, consumes one letter from the candidate word belonging to the set (`;ca[tr]` → `act`, `art`, `car`, `cat`, `rat`, `tar`). When multiple choice lists appear in an anagram (e.g., `;[ab][bc][cd]`), the engine must solve a **bipartite matching** problem (via backtracking or max-flow) assigning distinct candidate letter occurrences to each choice list, subject to the hybrid omission budget.
- **Validation Constraints**:
  - Only alphabetic characters recognized by the active dictionary may appear inside `[...]`, optionally preceded by `^` at the very first position after `[`. Any digit, space, punctuation character, backslash, nested `[`, or non-initial `^` raises error `2`.
  - Duplicate canonical letters inside a choice list (`[aab]` or `[eé]`) are deduplicated.
  - An empty list (`[]`, `[^]`) or a negated list that excludes every letter of the dictionary's alphabet raises error `14`.
  - Unclosed `[` raises error `5`; unmatched `]` outside a choice list raises error `9`; unescaped `^` outside `[...]` raises error `12`.

### 6.4 Letter Variables (`0`–`9`)
Unescaped digits `0` through `9` act as **unification variables**, each representing a single unknown canonical letter:
- **Unification Scope**: Within a single `AtomicPattern`, every occurrence of the same digit—across its `<template>`, its `;<anagram>`, and any nested `(sub-patterns)`—must bind to the **exact same canonical letter**:
  - Template repetition: `11` matches 2-letter doublets; `*1122*` matches `balloon`, `bookkeeper`, `committee`; `12321` matches 5-letter palindromes (`radar`, `level`, `kayak`).
  - Anagram repetition: `;112233` matches 6-letter words composed of three pairs of letters (`deeded`, `murmur`, `tartan`).
  - Cross-template/anagram binding: `1....1;111*` requires a 6-letter word starting and ending with the same letter `1` and containing at least three copies of `1` overall.
  - Note on hybrid omissions: In an anagram with a positive omission budget, variable occurrences in `<anagram>` represent required copies of that bound letter subject to the remaining omission budget.
- **Exclusive vs. Non-Exclusive Mode (`Exclusive` / `/X`)**:
  - **Non-Exclusive (`Exclusive = false`, `/X:0`)**: Distinct digits *may* bind to the same canonical letter (`1221` matches `oooo` with `1=o, 2=o` as well as `deed` and `noon`; `1212` matches `aaaa` as well as `papa`).
  - **Exclusive (`Exclusive = true`, `/X:1`)**: Distinct digits `0`–`9` within the same `AtomicPattern` are forced to bind to **distinct** canonical letters (`1212` matches `papa` and `tutu`, but rejects `aaaa` because `1` and `2` cannot bind to the same letter).
- **Independence Across Boolean Operators**: Each leaf `AtomicPattern` in a `&` or `|` expression has an **independent** variable environment. In `11* & *11`, the first `1` and the second `1` do **not** unify (matching words like `assess` or `sleepless` that start with one doubled letter and end with a possibly different doubled letter).

### 6.5 Embedded Sub-Patterns (`(...)`)
Parentheses inside a `<template>` or `<anagram>` embed a complete recursive `AtomicPattern` (`[template][;anagram]`) that matches a **contiguous slice** of the candidate word's canonical letters:
- **In a Template**: Matches a contiguous substring at that position in the template:
  - `(;orange)*` — words starting with any 6-letter permutation of `orange` (`orangeade`).
  - `*(;post)*` — words containing any contiguous 4-letter permutation of `post` (`apostle`, `unstoppable`, `compost`).
  - `(1221)*` — words starting with a 4-letter palindrome.
- **In an Anagram**: Requires that **some contiguous slice** of unclaimed positions in the candidate word matches the sub-pattern; the letters in that matched slice are consumed from the candidate word's available letter pool before matching the rest of `<anagram>`:
  - `;balderd(ash)` — rearrangements of the 10 letters `b,a,l,d,e,r,d,a,s,h` in which `ash` appears contiguously (`balderdash`).
  - `;listen(in.)` — rearrangements of `listen` containing a 3-letter contiguous slice matching `in.` (`inlets`, `tinsel`, whereas `listen` and `silent` do not match).
  - Note: Sub-patterns inside `<anagram>` are not eligible to be skipped by the omission budget—each embedded sub-pattern must match a contiguous slice of the candidate word, and its consumed letters reduce the remaining candidate letters tested against the explicit letter/choice/variable pool.
- **Nesting**: Sub-patterns may nest arbitrarily, subject to the 256-slot sub-pattern/choice-list limit per `AtomicPattern` (error `3`).

---

## 7. Escapes (`\`), Case/Accent Exactness, and Punctuation Matching

### 7.1 Backslash Escapes (`\c`) in Templates
Inside a `<template>`, `\` escapes the single byte `c` immediately following it (a trailing lone `\` at the end of a pattern raises error `13`):

1. **`c` is an Alphabetic Character** (e.g., `\A`, `\c`, `\é`):
   - Matches one canonical letter at the current template position whose equivalence class equals `letter_idx(c)`, **and** requires that the raw, unfolded byte in the candidate word's display string at that letter's position equals `c` byte-for-byte.
   - Examples: `\Man*` matches `Manchester` but rejects `manchester`; `caf\é` matches `café` but rejects `cafe` and `CAFÉ`.
   - **Important**: Escaping an alphabetic letter does **not** treat punctuation characters as active when `IgnoreUnusedPunctuation` is `true` (`\McDonalds` still matches `McDonald's` when `IgnoreUnusedPunctuation = true`).
2. **`c` is a Non-Alphabetic Character** (e.g., `\.`, `\?`, `\*`, `\;`, `\[`, `\]`, `\(`, `\)`, `\^`, `\\`, `\&`, `\|`, `\!`, `\@`, `\#`, `\0`–`\9`, `\-`, `\'`, `\ `):
   - Suppresses any special operator/wildcard/variable meaning of `c` and matches `c` as **literal template punctuation** at the exact boundary between surrounding template letters.

### 7.2 Unescaped Punctuation & `IgnoreUnusedPunctuation`

Any unescaped non-alphabetic character in `<template>` that is not a syntax metacharacter (such as space `' '`, hyphen `'-'`, apostrophe `'\''`, slash `'/'`) is also treated as **literal template punctuation** at that boundary.

How punctuation characters in the candidate display string that are **not** mentioned in `<template>` behave is controlled by **`IgnoreUnusedPunctuation`**:

| Setting | Active Punctuation Set | Matching Semantics | Example: Pattern `...-....` |
| :--- | :--- | :--- | :--- |
| **Ignore Unused Punctuation = `true`** (`TEA.exe` default, `/U:1`) | Only the specific punctuation byte values explicitly present in `<template>`. All other punctuation bytes are ignored. | Every occurrence of an *active* punctuation byte in the candidate word must match 1-for-1 at the exact letter boundaries specified in `<template>` (including before letter `0` and after letter `L - 1`). Unmentioned punctuation bytes may appear freely anywhere. | `cul-de-sac` → **No** (second `-` is active and unmatched).<br>`son-in law` → **Yes** (single `-` is after letter 3; space `' '` is unused so ignored). |
| **Ignore Unused Punctuation = `false`** (`TEACON.exe` default, `/U:0`) | If *any* punctuation or escaped character appears in `<template>`, **every** non-alphabetic byte value in the dictionary alphabet becomes active. (If `<template>` has zero punctuation/escapes, all punctuation remains ignored.) | **Every** space, hyphen, apostrophe, or symbol in the candidate display string must match 1-for-1 at the exact boundaries in `<template>`. | `bye-laws` → **Yes** (exact).<br>`son-in law` → **No** (unmatched space `' '`). |

> [!NOTE]
> Inside `;<anagram>`, neither backslash escapes nor punctuation characters (other than ignored ASCII space `0x20`) are permitted; any punctuation, `[`, `^`, or `\` in `<anagram>` raises validation error `11`.

---

## 8. Limits, Validation Errors & Length Pre-Checking

### 8.1 Engine Limits
- **Dictionary Word Lengths**: `1` to `100` canonical letters.
- **Template Minimum Length**: A compiled `<template>` (or sub-pattern) whose minimum matched canonical length exceeds `128` raises validation error `4`.
- **Complexity Pool Limit**: Each `AtomicPattern` supports up to `256` total choice-list bitmaps and compiled sub-patterns; exceeding this raises error `3`.
- **Template Non-Alphabetic Limit**: Excess punctuation/escape constraints within a single template span exceeding internal span encoding bounds raise error `10`.
- **Logical Nesting Depth**: Up to `32` nested logical `(...)` groups.

### 8.2 Validation Status Codes

Every `AtomicPattern` (and the enclosing `Query`) produces one of 15 validation status codes:

| Code | Condition | Diagnostic Message |
| :---: | :--- | :--- |
| `0` | Valid pattern | *Pattern is valid - press the return key to start searching* |
| `1` | Empty query or empty operand around `&`, `\|`, `!`, `()` | *Pattern is empty* |
| `2` | Non-letter (or non-initial `^` / nested `[`) inside `[...]` | *Choice lists can only include letters, optionally preceded by `^`* |
| `3` | Choice-list + sub-pattern count exceeds `256` in an atomic pattern | *Too many patterns or choice lists* |
| `4` | Minimum matched canonical word length `> 128` | *Pattern is too long to match anything* |
| `5` | Unclosed `[` | *Choice list started - use `]` to complete it* |
| `6` | Second unescaped `;` in the same pattern or sub-pattern | *Only one `;` is allowed in a pattern* |
| `7` | Unclosed `(` in sub-pattern or logical group | *Subpattern started - use `)` to complete it* |
| `8` | Unmatched `)` or invalid trailing tokens after sub-pattern | *A `)` can only appear at the end of a subpattern* |
| `9` | Unmatched `]` outside a choice list | *A `]` can only appear at the end of a choice list* |
| `10` | Excess punctuation/escaped characters in template | *Too many non-alphabetic characters in templates* |
| `11` | Punctuation (other than `' '`), `^`, `[`, or `\` inside `;anagram` | *Only letters and match-alls allowed in anagrams* |
| `12` | Unescaped `^` outside `[...]` | *A `^` can only appear within a choice list* |
| `13` | Trailing lone `\` at end of pattern | *Follow `\` by the character you want to match exactly* |
| `14` | Empty `[]`, `[^]`, or negated choice list excluding entire alphabet | *Choice lists must contain at least one letter* |

### 8.3 Length Compatibility Pre-Check (and Original TEA Negation Quirk)

Before scanning dictionary length buckets in Word Search mode, TEA evaluates whether each candidate length $L \in [\text{shortest}, \text{longest}]$ can possibly satisfy the boolean query:
- If **zero** lengths in $[\text{shortest}, \text{longest}]$ are feasible (for example, `... & ....` where $L=3$ and $L=4$ are disjoint), the engine warns *"Patterns match incompatible lengths"* (or *"No length of word will be matched"*).

> [!WARNING]
> **Compatibility Note on Negated Length Pre-Filtering**:
> In `TEA.exe` / `TEACON.exe` v2.1, the length-feasibility pre-check evaluates `[L_min, L_max]` intervals across `&` and `|` while **ignoring the `!` (NOT) inversion flag** (originally done so standalone `!*ing` wouldn't mark lengths `3..100` infeasible). As an unintended side effect in original TEA, `.. & !*ing` checks `L = 2` against `[2..2] & [3..100]`, evaluates to `false`, and skips 2-letter words!
>
> A clean compatible implementation should evaluate negated sub-expressions during length pre-filtering as **potentially matching all lengths `[1..100]`** (unless the negated sub-expression unconditionally matches *every* possible word of length $L$, such as `!...` at $L = 3$).

---

## 9. Result Formatting, Filtering, Deduplication & Sorting

### 9.1 Anagram Balance (`+added` / `-omitted`)

When an `AtomicPattern` containing an `;<anagram>` matches a word and `ShowBalances` is active, the engine computes a canonical letter multiset delta `balance[c]` for each canonical letter `c` in `0 .. num_used_letters - 1`:
1. Add `+1` to `balance[c]` for every canonical letter occurrence in the matched candidate word.
2. Subtract `-1` from `balance[c]` for every explicit letter, bound letter variable, matched choice-list letter, and matched sub-pattern letter consumed from `<anagram>`.
3. If any `balance[c] != 0`, format the balance suffix in **canonical alphabet order**:
   - Negative counts (`balance[c] < 0`, omitted rack letters) are listed after `-`, repeating `c` `|balance[c]|` times.
   - Positive counts (`balance[c] > 0`, added blank/wildcard letters) are listed after `+`, repeating `c` `balance[c]` times.
   - Examples: `hitter -K+R` (for `......*;ehiktt.`), `aorta -DM` (for `.....*;aadmort`), `sports +RS` (for `;post*`).

### 9.2 Entry Category Filter

Matched display words can be filtered into four mutually toggleable categories based on their raw display characters:
1. **Lowercase words**: Contains only lowercase alphabetic letters (no uppercase letters, spaces, or punctuation).
2. **Capitalized / Uppercase words**: Contains at least one uppercase alphabetic letter and no spaces or punctuation (proper nouns, abbreviations, acronyms).
3. **Words containing punctuation**: Single-token entries containing non-alphabetic punctuation such as hyphens, apostrophes, or slashes, with **no spaces** (`' '`).
4. **Phrases**: Multi-word entries containing at least one space (`' '`).

### 9.3 Multi-Dictionary Duplicate Suppression

When searching across an ordered list of dictionaries with duplicate suppression enabled, headwords are searched in dictionary priority order; once a normalized headword has been emitted by a higher-priority dictionary, identical entries in lower-priority dictionaries are suppressed.

### 9.4 Result Sort Modes

TEA defines six result ordering modes:
1. **Dictionary Order (`0`)**: Preserves dictionary priority order, length-bucket order, and stream order within each bucket.
2. **Alphabetical Ascending (`1`)**: Case- and accent-folded canonical alphabetical order.
3. **Alphabetical Descending (`2`)**: Reverse canonical alphabetical order.
4. **Word Length Ascending (`3`)**: Canonical letter count $L$ ascending, then alphabetical ascending within each length.
5. **Word Length Descending (`5`)**: Canonical letter count $L$ descending, then alphabetical ascending within each length.
6. **Smart Length Order (`4`)**: Orders results by **Anagram Balance length ascending** (total count of `+added` and `-omitted` letters ascending, with alphabetical tie-breaking). This single rule naturally places:
   - Exact anagrams (`0` balance letters) at the very top,
   - Longest sub-anagrams first when building words from a rack (`*;aadmort` puts 7-letter `matador` before 6-letter `-1` words, before 5-letter `-2` words), and
   - Shortest super-anagrams first when extending letters (`;post*` puts 4-letter `post`/`stop` before 5-letter `+1` words, before 6-letter `+2` words).

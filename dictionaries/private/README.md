# Private Dictionaries

Any dictionaries placed in this directory (`dictionaries/private/`) will be included in the generated installer and release packages alongside the public dictionaries in `dictionaries/`, but will not be committed to version control (`.gitignore` ignores all files here except this `README.md`).

When packaged, private dictionaries are placed in the same `dictionaries/` output directory as non-private dictionaries. If a file with the same name appears in both `dictionaries/` and `dictionaries/private/`, the version in this directory takes precedence and the other is discarded.

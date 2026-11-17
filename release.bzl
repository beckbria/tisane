"""Bazel rules for packaging Tisane Linux (.tar.gz) and Windows (.zip) release archives."""

def _release_archive_impl(ctx):
    out = ctx.outputs.out
    args = ctx.actions.args()
    args.add("-format", ctx.attr.format)
    args.add("-out", out.path)

    for b in ctx.files.binaries:
        args.add("-bin", b.path + "=" + b.basename)

    for d in ctx.files.dictionaries:
        rel = d.short_path
        if not rel.startswith("dictionaries/"):
            rel = "dictionaries/" + d.basename
        args.add("-file", d.path + "=" + rel)

    ctx.actions.run(
        executable = ctx.executable._packager,
        arguments = [args],
        inputs = depset(ctx.files.binaries + ctx.files.dictionaries),
        outputs = [out],
        mnemonic = "ReleaseArchive",
        progress_message = "Packaging release archive %s" % out.short_path,
    )
    return [DefaultInfo(files = depset([out]))]

release_archive = rule(
    implementation = _release_archive_impl,
    attrs = {
        "binaries": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "dictionaries": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "format": attr.string(
            values = [
                "tar.gz",
                "zip",
            ],
            mandatory = True,
        ),
        "out": attr.output(
            mandatory = True,
        ),
        "_packager": attr.label(
            default = "//internal/releasepkg",
            executable = True,
            cfg = "exec",
        ),
    },
)

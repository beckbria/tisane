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

    extra_inputs = []
    for target, dst in ctx.attr.extra_files.items():
        files = target.files.to_list()
        if len(files) != 1:
            fail("extra_files entry %s must provide exactly one file, got %d" % (target.label, len(files)))
        f = files[0]
        args.add("-file", f.path + "=" + dst)
        extra_inputs.append(f)

    ctx.actions.run(
        executable = ctx.executable._packager,
        arguments = [args],
        inputs = depset(ctx.files.binaries + ctx.files.dictionaries + extra_inputs),
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
        "extra_files": attr.label_keyed_string_dict(
            allow_files = True,
            default = {},
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

"""Bazel rules for packaging Tisane Linux (.tar.gz) and Windows (.exe installer / .zip) releases."""

def _collect_dictionaries(ctx):
    """Collects dictionary (dst_rel, src_file) pairs, with private_dictionaries overriding dictionaries."""
    by_rel = {}

    for d in ctx.files.private_dictionaries:
        rel = d.short_path
        if rel.startswith("dictionaries/private/"):
            rel = "dictionaries/" + rel[len("dictionaries/private/"):]
        else:
            rel = "dictionaries/" + d.basename
        by_rel[rel] = d

    for d in ctx.files.dictionaries:
        rel = d.short_path
        if not rel.startswith("dictionaries/"):
            rel = "dictionaries/" + d.basename
        if rel not in by_rel:
            by_rel[rel] = d

    return sorted(by_rel.items(), key = lambda item: item[0])

def _release_archive_impl(ctx):
    out = ctx.outputs.out
    args = ctx.actions.args()
    args.add("-format", ctx.attr.format)
    args.add("-out", out.path)

    for b in ctx.files.binaries:
        args.add("-bin", b.path + "=" + b.basename)

    dict_entries = _collect_dictionaries(ctx)
    dict_files = []
    for rel, d in dict_entries:
        args.add("-file", d.path + "=" + rel)
        dict_files.append(d)

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
        inputs = depset(ctx.files.binaries + dict_files + extra_inputs),
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
        "private_dictionaries": attr.label_list(
            allow_files = True,
            default = [],
        ),
        "_packager": attr.label(
            default = "//internal/releasepkg",
            executable = True,
            cfg = "exec",
        ),
    },
)

def _windows_installer_impl(ctx):
    out = ctx.outputs.out
    args = ctx.actions.args()
    args.add("-script", ctx.file.script.path)
    args.add("-icon", ctx.file.icon.path)
    args.add("-out", out.path)
    if ctx.attr.version:
        args.add("-version", ctx.attr.version)

    for b in ctx.files.binaries:
        args.add("-bin", b.path + "=" + b.basename)

    dict_entries = _collect_dictionaries(ctx)
    dict_files = []
    for rel, d in dict_entries:
        args.add("-file", d.path + "=" + rel)
        dict_files.append(d)

    ctx.actions.run(
        executable = ctx.executable._builder,
        arguments = [args],
        inputs = depset(
            ctx.files.binaries +
            dict_files +
            [ctx.file.icon, ctx.file.script],
        ),
        outputs = [out],
        mnemonic = "WindowsInstaller",
        progress_message = "Building Windows installer %s" % out.short_path,
        use_default_shell_env = True,
    )
    return [DefaultInfo(files = depset([out]))]

windows_installer = rule(
    implementation = _windows_installer_impl,
    attrs = {
        "binaries": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "dictionaries": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "icon": attr.label(
            allow_single_file = [".png"],
            mandatory = True,
        ),
        "out": attr.output(
            mandatory = True,
        ),
        "private_dictionaries": attr.label_list(
            allow_files = True,
            default = [],
        ),
        "script": attr.label(
            allow_single_file = [".nsi"],
            mandatory = True,
        ),
        "version": attr.string(
            default = "",
        ),
        "_builder": attr.label(
            default = "//internal/wininstaller",
            executable = True,
            cfg = "exec",
        ),
    },
)

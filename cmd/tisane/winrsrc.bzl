"""Rule to generate a Windows COFF resource object (.syso) from build assets."""

def _windows_syso_impl(ctx):
    out = ctx.outputs.out
    args = ctx.actions.args()
    inputs = [ctx.file.icon]

    args.add("-icon", ctx.file.icon.path)
    if ctx.file.manifest:
        args.add("-manifest", ctx.file.manifest.path)
        inputs.append(ctx.file.manifest)
    if ctx.file.info:
        args.add("-info", ctx.file.info.path)
        inputs.append(ctx.file.info)
    args.add("-arch", ctx.attr.arch)
    args.add("-out", out.path)

    ctx.actions.run(
        executable = ctx.executable._generator,
        arguments = [args],
        inputs = inputs,
        outputs = [out],
        mnemonic = "WinRsrc",
        progress_message = "Generating Windows resource object %s" % out.short_path,
    )
    return [DefaultInfo(files = depset([out]))]

windows_syso = rule(
    implementation = _windows_syso_impl,
    attrs = {
        "arch": attr.string(
            default = "amd64",
            values = [
                "amd64",
                "arm64",
                "386",
            ],
        ),
        "icon": attr.label(
            allow_single_file = [".png"],
            mandatory = True,
        ),
        "info": attr.label(
            allow_single_file = [".json"],
        ),
        "manifest": attr.label(
            allow_single_file = [
                ".manifest",
                ".xml",
            ],
        ),
        "out": attr.output(
            mandatory = True,
        ),
        "_generator": attr.label(
            default = "//cmd/tisane/internal/winrsrc",
            executable = True,
            cfg = "exec",
        ),
    },
)

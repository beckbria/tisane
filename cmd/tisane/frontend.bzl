"""Rule to build the Svelte 5 frontend bundle (frontend/dist) for go:embed."""

def _frontend_dist_impl(ctx):
    out_dir = ctx.actions.declare_directory("frontend/dist")
    pkg_json = ctx.file.package_json
    node_toolchain = ctx.toolchains["@rules_nodejs//nodejs:toolchain_type"].nodeinfo
    node_bin = node_toolchain.target_tool_path
    ctx.actions.run(
        executable = ctx.executable._builder,
        arguments = ["-node", node_bin, pkg_json.path, out_dir.path],
        inputs = ctx.files.srcs + node_toolchain.tool_files,
        outputs = [out_dir],
        execution_requirements = {
            "no-sandbox": "1",
        },
        use_default_shell_env = True,
        mnemonic = "ViteBuild",
        progress_message = "Building Svelte 5 frontend bundle (frontend/dist)",
    )
    return [DefaultInfo(files = depset([out_dir]))]

frontend_dist = rule(
    implementation = _frontend_dist_impl,
    attrs = {
        "package_json": attr.label(
            default = ":frontend/package.json",
            allow_single_file = True,
        ),
        "srcs": attr.label_list(
            allow_files = True,
            mandatory = True,
        ),
        "_builder": attr.label(
            default = "//cmd/tisane/internal/vitebuild",
            executable = True,
            cfg = "exec",
        ),
    },
    toolchains = ["@rules_nodejs//nodejs:toolchain_type"],
)

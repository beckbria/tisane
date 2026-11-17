"""Rule to build the Svelte 5 frontend bundle (frontend/dist) for go:embed."""

def _frontend_dist_impl(ctx):
    out_dir = ctx.actions.declare_directory("frontend/dist")
    pkg_json = ctx.file.package_json
    ctx.actions.run_shell(
        inputs = ctx.files.srcs,
        outputs = [out_dir],
        command = """
            set -e
            REAL_FRONTEND_DIR="$(dirname "$(readlink -f "{pkg_json}")")"
            FRONTEND_SRC="$(cd "$(dirname "{pkg_json}")" && pwd)"
            OUT_DIST="$(pwd)/{out_dir}"
            TMP_DIR="$(mktemp -d)"
            trap 'rm -rf "$TMP_DIR"' EXIT
            cp -R "$FRONTEND_SRC/." "$TMP_DIR/"
            if [ -d "$REAL_FRONTEND_DIR/node_modules" ]; then
                ln -s "$REAL_FRONTEND_DIR/node_modules" "$TMP_DIR/node_modules"
            else
                (cd "$TMP_DIR" && npm ci --no-audit --no-fund)
            fi
            (cd "$TMP_DIR" && ./node_modules/.bin/vite build --outDir "$OUT_DIST" --emptyOutDir)
        """.format(
            pkg_json = pkg_json.path,
            out_dir = out_dir.path,
        ),
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
    },
)

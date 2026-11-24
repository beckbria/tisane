// Command wininstaller is a Bazel build helper that stages compiled Tisane
// Windows binaries and bundled dictionaries, converts the PNG application icon
// into a multi-resolution Windows .ico file, generates NSIS install/uninstall
// file lists, and invokes makensis to build a self-contained Windows installer.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"

	"tisane"
)

// iconSizes are the standard Windows 10/11 icon resolutions embedded in the
// installer/uninstaller .ico file.
var iconSizes = []int{256, 128, 64, 48, 32, 24, 16}

type stagedEntry struct {
	srcPath string
	dstPath string // normalized forward-slash relative path
}

type entrySliceFlag []string

func (f *entrySliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *entrySliceFlag) Set(val string) error {
	*f = append(*f, val)
	return nil
}

type config struct {
	scriptPath   string
	iconPath     string
	outPath      string
	appVersion   string
	makensisPath string
	bins         []string
	files        []string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "wininstaller: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("wininstaller", flag.ContinueOnError)
	var cfg config
	var bins entrySliceFlag
	var files entrySliceFlag

	fs.StringVar(&cfg.scriptPath, "script", "", "path to NSIS .nsi script")
	fs.StringVar(&cfg.iconPath, "icon", "", "path to source PNG icon")
	fs.StringVar(&cfg.outPath, "out", "", "output Windows installer .exe path")
	fs.StringVar(&cfg.appVersion, "version", tisane.Version, "application version string")
	fs.StringVar(&cfg.makensisPath, "makensis", "", "path to makensis executable (defaults to PATH lookup)")
	fs.Var(&bins, "bin", "executable binary mapping in src=dst format (repeatable)")
	fs.Var(&files, "file", "regular file mapping in src=dst format (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.bins = bins
	cfg.files = files

	return buildInstaller(cfg)
}

func buildInstaller(cfg config) error {
	if cfg.scriptPath == "" {
		return fmt.Errorf("-script is required")
	}
	if cfg.iconPath == "" {
		return fmt.Errorf("-icon is required")
	}
	if cfg.outPath == "" {
		return fmt.Errorf("-out is required")
	}
	if strings.TrimSpace(cfg.appVersion) == "" {
		cfg.appVersion = tisane.Version
	}

	entries, err := parseEntries(cfg.bins, cfg.files)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("at least one -bin or -file entry is required")
	}

	makensisBin := strings.TrimSpace(cfg.makensisPath)
	if makensisBin == "" {
		var lookErr error
		makensisBin, lookErr = exec.LookPath("makensis")
		if lookErr != nil {
			return fmt.Errorf("makensis not found in PATH; install NSIS (e.g. 'sudo apt install nsis' on Debian/Ubuntu): %w", lookErr)
		}
	}

	absScript, err := filepath.Abs(cfg.scriptPath)
	if err != nil {
		return fmt.Errorf("resolve script path: %w", err)
	}
	absOut, err := filepath.Abs(cfg.outPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	workDir, err := os.MkdirTemp("", "tisane-wininstaller-*")
	if err != nil {
		return fmt.Errorf("create temp workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	icoPath := filepath.Join(workDir, "tisane.ico")
	if err := writeICOFromPNG(cfg.iconPath, icoPath); err != nil {
		return err
	}

	stagingDir := filepath.Join(workDir, "staging")
	if err := stageEntries(stagingDir, entries); err != nil {
		return err
	}

	installNSH := filepath.Join(workDir, "files_install.nsh")
	uninstallNSH := filepath.Join(workDir, "files_uninstall.nsh")
	if err := os.WriteFile(installNSH, []byte(generateInstallNSH(stagingDir, entries)), 0o644); err != nil {
		return fmt.Errorf("write files_install.nsh: %w", err)
	}
	if err := os.WriteFile(uninstallNSH, []byte(generateUninstallNSH(entries)), 0o644); err != nil {
		return fmt.Errorf("write files_uninstall.nsh: %w", err)
	}

	versionQuad := formatVersionQuad(cfg.appVersion)
	cmdArgs := []string{
		"-V2",
		"-DAPP_VERSION=" + cfg.appVersion,
		"-DAPP_VERSION_QUAD=" + versionQuad,
		"-DICON_PATH=" + icoPath,
		"-DSTAGING_DIR=" + stagingDir,
		"-DFILES_INSTALL_NSH=" + installNSH,
		"-DFILES_UNINSTALL_NSH=" + uninstallNSH,
		"-DOUT_FILE=" + absOut,
		absScript,
	}

	cmd := exec.Command(makensisBin, cmdArgs...)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("makensis failed: %w\n%s", err, outBuf.String())
	}
	return nil
}

func parseEntries(bins, files []string) ([]stagedEntry, error) {
	seen := make(map[string]bool, len(bins)+len(files))
	entries := make([]stagedEntry, 0, len(bins)+len(files))

	add := func(raw string) error {
		idx := strings.LastIndex(raw, "=")
		if idx <= 0 || idx == len(raw)-1 {
			return fmt.Errorf("invalid mapping %q (expected <src>=<dst>)", raw)
		}
		src := filepath.FromSlash(raw[:idx])
		dst := path.Clean(strings.ReplaceAll(raw[idx+1:], "\\", "/"))
		dst = strings.TrimPrefix(dst, "/")
		if dst == "" || dst == "." || strings.HasPrefix(dst, "../") {
			return fmt.Errorf("invalid installer destination path %q", raw[idx+1:])
		}
		if seen[dst] {
			return fmt.Errorf("duplicate installer destination path %q", dst)
		}
		seen[dst] = true
		entries = append(entries, stagedEntry{
			srcPath: src,
			dstPath: dst,
		})
		return nil
	}

	for _, b := range bins {
		if err := add(b); err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		if err := add(f); err != nil {
			return nil, err
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].dstPath < entries[j].dstPath
	})
	return entries, nil
}

func stageEntries(stagingDir string, entries []stagedEntry) error {
	for _, e := range entries {
		dst := filepath.Join(stagingDir, filepath.FromSlash(e.dstPath))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create staging dir for %q: %w", e.dstPath, err)
		}
		if err := copyFile(e.srcPath, dst); err != nil {
			return fmt.Errorf("stage %q -> %q: %w", e.srcPath, e.dstPath, err)
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) (retErr error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

func writeICOFromPNG(pngPath, icoPath string) (retErr error) {
	f, err := os.Open(pngPath)
	if err != nil {
		return fmt.Errorf("open icon %q: %w", pngPath, err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return fmt.Errorf("decode PNG %q: %w", pngPath, err)
	}

	ico, err := winres.NewIconFromResizedImage(img, iconSizes)
	if err != nil {
		return fmt.Errorf("resize icon %q: %w", pngPath, err)
	}

	out, err := os.OpenFile(icoPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create ico %q: %w", icoPath, err)
	}
	defer func() {
		if cerr := out.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	if err := ico.SaveICO(out); err != nil {
		return fmt.Errorf("encode ico %q: %w", icoPath, err)
	}
	return nil
}

func formatVersionQuad(ver string) string {
	var quad [4]uint16
	if i := strings.IndexAny(ver, "-+"); i >= 0 {
		ver = ver[:i]
	}
	parts := strings.Split(ver, ".")
	for i := 0; i < len(parts) && i < 4; i++ {
		n, err := strconv.ParseUint(strings.TrimSpace(parts[i]), 10, 16)
		if err != nil {
			break
		}
		quad[i] = uint16(n)
	}
	return fmt.Sprintf("%d.%d.%d.%d", quad[0], quad[1], quad[2], quad[3])
}

// escapeNSIS escapes characters that are special inside double-quoted NSIS strings.
func escapeNSIS(s string) string {
	s = strings.ReplaceAll(s, "$", "$$")
	s = strings.ReplaceAll(s, `"`, `$\"`)
	return s
}

func toWindowsRelPath(rel string) string {
	return strings.ReplaceAll(rel, "/", `\`)
}

// generateInstallNSH emits SetOutPath and File directives grouped by directory.
func generateInstallNSH(stagingDir string, entries []stagedEntry) string {
	byDir := make(map[string][]stagedEntry)
	var dirs []string
	for _, e := range entries {
		dir := path.Dir(e.dstPath)
		if dir == "." {
			dir = ""
		}
		if _, ok := byDir[dir]; !ok {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], e)
	}
	sort.Strings(dirs)

	var sb strings.Builder
	sb.WriteString("; Auto-generated by wininstaller -- do not edit.\n")
	for _, dir := range dirs {
		if dir == "" {
			sb.WriteString("SetOutPath \"$INSTDIR\"\n")
		} else {
			sb.WriteString(fmt.Sprintf("SetOutPath \"$INSTDIR\\%s\"\n", escapeNSIS(toWindowsRelPath(dir))))
		}
		for _, e := range byDir[dir] {
			hostPath := filepath.Join(stagingDir, filepath.FromSlash(e.dstPath))
			sb.WriteString(fmt.Sprintf("File \"%s\"\n", escapeNSIS(hostPath)))
		}
	}
	return sb.String()
}

// generateUninstallNSH emits exact Delete directives for bundled files and
// non-recursive RMDir directives for subdirectories (deepest first) so
// user-added dictionaries are preserved on uninstall.
func generateUninstallNSH(entries []stagedEntry) string {
	dirSet := make(map[string]bool)
	var sb strings.Builder
	sb.WriteString("; Auto-generated by wininstaller -- do not edit.\n")

	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("Delete \"$INSTDIR\\%s\"\n", escapeNSIS(toWindowsRelPath(e.dstPath))))
		dir := path.Dir(e.dstPath)
		for dir != "." && dir != "/" && dir != "" {
			dirSet[dir] = true
			dir = path.Dir(dir)
		}
	}

	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	// Remove deepest directories first.
	sort.Slice(dirs, func(i, j int) bool {
		di := strings.Count(dirs[i], "/")
		dj := strings.Count(dirs[j], "/")
		if di != dj {
			return di > dj
		}
		return dirs[i] < dirs[j]
	})
	for _, d := range dirs {
		sb.WriteString(fmt.Sprintf("RMDir \"$INSTDIR\\%s\"\n", escapeNSIS(toWindowsRelPath(d))))
	}
	return sb.String()
}

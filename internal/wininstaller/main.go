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
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tc-hib/winres"

	"tisane"
	"tisane/internal/buildutil"
)

// iconSizes are the standard Windows 10/11 icon resolutions embedded in the
// installer/uninstaller .ico file.
var iconSizes = []int{256, 128, 64, 48, 32, 24, 16}

type stagedEntry struct {
	srcPath   string
	dstPath   string // normalized forward-slash relative path
	stageName string // safe flat filename in stagingDir (e.g. "stage_0000.bin")
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
	var bins buildutil.EntrySliceFlag
	var files buildutil.EntrySliceFlag

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
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	cfg.bins = bins
	cfg.files = files

	return buildInstaller(cfg)
}

func validateNSISDefine(name, val string) error {
	if strings.ContainsAny(val, "\r\n\"$") {
		return fmt.Errorf("NSIS define %s contains unsafe character in %q", name, val)
	}
	return nil
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

	versionQuad, err := buildutil.FormatVersionQuad(cfg.appVersion)
	if err != nil {
		return err
	}
	if err := validateNSISDefine("APP_VERSION", cfg.appVersion); err != nil {
		return err
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
	outDir := filepath.Dir(absOut)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
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

	tmpOutFile, err := os.CreateTemp(outDir, ".tisane-installer-*.exe.tmp")
	if err != nil {
		return fmt.Errorf("create temp installer output: %w", err)
	}
	tmpOutPath := tmpOutFile.Name()
	_ = tmpOutFile.Close()
	keepTmpOut := false
	defer func() {
		if !keepTmpOut {
			_ = os.Remove(tmpOutPath)
		}
	}()

	for name, val := range map[string]string{
		"ICON_PATH":           icoPath,
		"STAGING_DIR":         stagingDir,
		"FILES_INSTALL_NSH":   installNSH,
		"FILES_UNINSTALL_NSH": uninstallNSH,
		"OUT_FILE":            tmpOutPath,
	} {
		if err := validateNSISDefine(name, val); err != nil {
			return err
		}
	}

	cmdArgs := []string{
		"-V2",
		"-DAPP_VERSION=" + cfg.appVersion,
		"-DAPP_VERSION_QUAD=" + versionQuad,
		"-DICON_PATH=" + icoPath,
		"-DSTAGING_DIR=" + stagingDir,
		"-DFILES_INSTALL_NSH=" + installNSH,
		"-DFILES_UNINSTALL_NSH=" + uninstallNSH,
		"-DOUT_FILE=" + tmpOutPath,
		absScript,
	}

	cmd := exec.Command(makensisBin, cmdArgs...)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("makensis failed: %w\n%s", err, outBuf.String())
	}
	if err := os.Rename(tmpOutPath, absOut); err != nil {
		return fmt.Errorf("rename installer output: %w", err)
	}
	keepTmpOut = true
	return nil
}

func parseEntries(bins, files []string) ([]stagedEntry, error) {
	parsed, err := buildutil.ParseEntries(bins, files)
	if err != nil {
		return nil, err
	}
	entries := make([]stagedEntry, len(parsed))
	for i, p := range parsed {
		entries[i] = stagedEntry{
			srcPath:   p.SrcPath,
			dstPath:   p.DstPath,
			stageName: fmt.Sprintf("stage_%04d.bin", i),
		}
	}
	return entries, nil
}

func stageEntries(stagingDir string, entries []stagedEntry) error {
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	for i := range entries {
		if entries[i].stageName == "" {
			entries[i].stageName = fmt.Sprintf("stage_%04d.bin", i)
		}
		dst := filepath.Join(stagingDir, entries[i].stageName)
		if err := buildutil.CopyRegularFile(entries[i].srcPath, dst); err != nil {
			return fmt.Errorf("stage %q -> %q: %w", entries[i].srcPath, entries[i].dstPath, err)
		}
	}
	return nil
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
	q, err := buildutil.FormatVersionQuad(ver)
	if err != nil {
		return "0.0.0.0"
	}
	return q
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

// generateInstallNSH emits SetOutPath and File /oname=... directives grouped by directory
// using safe sequential stage filenames so special characters in target names never confuse makensis.
func generateInstallNSH(stagingDir string, entries []stagedEntry) string {
	byDir := make(map[string][]stagedEntry)
	var dirs []string
	for i, e := range entries {
		if e.stageName == "" {
			e.stageName = fmt.Sprintf("stage_%04d.bin", i)
		}
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
			baseName := path.Base(e.dstPath)
			hostPath := filepath.Join(stagingDir, e.stageName)
			sb.WriteString(fmt.Sprintf("File \"/oname=%s\" \"%s\"\n", escapeNSIS(baseName), escapeNSIS(hostPath)))
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

package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFormatVersionQuad(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0.3.0", "0.3.0.0"},
		{"1.2.3.4", "1.2.3.4"},
		{"2.5.1-rc1+build9", "2.5.1.0"},
		{"3", "3.0.0.0"},
		{"", "0.0.0.0"},
	}
	for _, tc := range tests {
		if got := formatVersionQuad(tc.in); got != tc.want {
			t.Errorf("formatVersionQuad(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseEntries(t *testing.T) {
	entries, err := parseEntries(
		[]string{"/tmp/tisane.exe=tisane.exe", "/tmp/cli.exe=tisane-cli.exe"},
		[]string{"/tmp/d.tsd=dictionaries/english.tsd"},
	)
	if err != nil {
		t.Fatalf("parseEntries returned unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Sorted by dstPath.
	wantDsts := []string{"dictionaries/english.tsd", "tisane-cli.exe", "tisane.exe"}
	for i, want := range wantDsts {
		if entries[i].dstPath != want {
			t.Errorf("entries[%d].dstPath = %q, want %q", i, entries[i].dstPath, want)
		}
	}

	if _, err := parseEntries([]string{"a=foo.exe", "b=foo.exe"}, nil); err == nil {
		t.Error("expected error for duplicate destination path")
	}
	if _, err := parseEntries([]string{"a=../escape.exe"}, nil); err == nil {
		t.Error("expected error for path traversal destination")
	}
	if _, err := parseEntries([]string{"invalid_mapping"}, nil); err == nil {
		t.Error("expected error for missing '=' separator")
	}
}

func TestGenerateInstallAndUninstallNSH(t *testing.T) {
	entries := []stagedEntry{
		{srcPath: "/src/a.tsd", dstPath: "dictionaries/sub/a$1.tsd", stageName: "stage_0000.bin"},
		{srcPath: "/src/b.tsd", dstPath: "dictionaries/b.tsd", stageName: "stage_0001.bin"},
		{srcPath: "/src/tisane.exe", dstPath: "tisane.exe", stageName: "stage_0002.bin"},
	}

	stageDir := filepath.Join("tmp", "stage")
	obsolete := []string{"dictionaries/english-wordnet-2025-plus.tsd"}
	installNSH := generateInstallNSH(stageDir, entries, obsolete)
	for _, want := range []string{
		`Delete "$INSTDIR\dictionaries\english-wordnet-2025-plus.tsd"`,
		`SetOutPath "$INSTDIR"`,
		`File "/oname=tisane.exe" "` + filepath.Join(stageDir, "stage_0002.bin") + `"`,
		`SetOutPath "$INSTDIR\dictionaries"`,
		`File "/oname=b.tsd" "` + filepath.Join(stageDir, "stage_0001.bin") + `"`,
		`SetOutPath "$INSTDIR\dictionaries\sub"`,
		`File "/oname=a$$1.tsd" "` + filepath.Join(stageDir, "stage_0000.bin") + `"`,
	} {
		if !strings.Contains(installNSH, want) {
			t.Errorf("install NSH missing %q:\n%s", want, installNSH)
		}
	}

	uninstallNSH := generateUninstallNSH(entries, obsolete)
	for _, want := range []string{
		`Delete "$INSTDIR\tisane.exe"`,
		`Delete "$INSTDIR\dictionaries\b.tsd"`,
		`Delete "$INSTDIR\dictionaries\sub\a$$1.tsd"`,
		`Delete "$INSTDIR\dictionaries\english-wordnet-2025-plus.tsd"`,
		`RMDir "$INSTDIR\dictionaries\sub"`,
		`RMDir "$INSTDIR\dictionaries"`,
	} {
		if !strings.Contains(uninstallNSH, want) {
			t.Errorf("uninstall NSH missing %q:\n%s", want, uninstallNSH)
		}
	}

	subIdx := strings.Index(uninstallNSH, `RMDir "$INSTDIR\dictionaries\sub"`)
	parentIdx := strings.Index(uninstallNSH, `RMDir "$INSTDIR\dictionaries"`)
	if subIdx == -1 || parentIdx == -1 || subIdx >= parentIdx {
		t.Errorf("expected deepest directory RMDir before parent:\n%s", uninstallNSH)
	}
}

func TestParseObsoletePaths(t *testing.T) {
	entries := []stagedEntry{
		{dstPath: "dictionaries/English Wordnet 2025 Plus.tsd"},
		{dstPath: "tisane.exe"},
	}

	got, err := parseObsoletePaths([]string{
		"dictionaries/english-wordnet-2025-plus.tsd",
		"dictionaries\\ENGLISH-WORDNET-2025-PLUS.TSD",
	}, entries)
	if err != nil {
		t.Fatalf("parseObsoletePaths unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "dictionaries/english-wordnet-2025-plus.tsd" {
		t.Fatalf("parseObsoletePaths = %v, want [dictionaries/english-wordnet-2025-plus.tsd]", got)
	}

	// Reject case-insensitive collision with an active staged entry.
	if _, err := parseObsoletePaths([]string{"dictionaries/english wordnet 2025 plus.tsd"}, entries); err == nil {
		t.Error("expected error when -obsolete collides case-insensitively with active staged file")
	}
	if _, err := parseObsoletePaths([]string{"../outside.tsd"}, entries); err == nil {
		t.Error("expected error for path traversal in -obsolete")
	}
}

func writeTestPNG(t *testing.T, p string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 120, B: 200, A: 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create test PNG: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
}

func TestWriteICOFromPNG(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "icon.png")
	icoPath := filepath.Join(dir, "icon.ico")
	writeTestPNG(t, pngPath)

	if err := writeICOFromPNG(pngPath, icoPath); err != nil {
		t.Fatalf("writeICOFromPNG failed: %v", err)
	}

	data, err := os.ReadFile(icoPath)
	if err != nil {
		t.Fatalf("read generated ico: %v", err)
	}
	if len(data) < 6 {
		t.Fatalf("ico too small: %d bytes", len(data))
	}
	reserved := binary.LittleEndian.Uint16(data[0:2])
	icoType := binary.LittleEndian.Uint16(data[2:4])
	count := binary.LittleEndian.Uint16(data[4:6])
	if reserved != 0 || icoType != 1 {
		t.Errorf("invalid ICO header: reserved=%d type=%d (want 0, 1)", reserved, icoType)
	}
	if int(count) != len(iconSizes) {
		t.Errorf("ICO image count = %d, want %d", count, len(iconSizes))
	}
}

func TestBuildInstallerWithFakeMakensis(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell stub for makensis")
	}

	dir := t.TempDir()
	pngPath := filepath.Join(dir, "appicon.png")
	writeTestPNG(t, pngPath)

	scriptPath := filepath.Join(dir, "installer.nsi")
	if err := os.WriteFile(scriptPath, []byte("; dummy script\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(dir, "tisane.exe")
	if err := os.WriteFile(binPath, []byte("MZ-stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	dictPath := filepath.Join(dir, "english.tsd")
	if err := os.WriteFile(dictPath, []byte("TSD1-stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	argsLog := filepath.Join(dir, "makensis.args")
	fakeMakensis := filepath.Join(dir, "fake-makensis.sh")
	stubScript := `#!/bin/sh
set -eu
out=""
for arg in "$@"; do
  printf '%s\n' "$arg" >> "` + argsLog + `"
  case "$arg" in
    -DOUT_FILE=*) out="${arg#-DOUT_FILE=}" ;;
    -DICON_PATH=*) test -s "${arg#-DICON_PATH=}" ;;
    -DFILES_INSTALL_NSH=*) test -s "${arg#-DFILES_INSTALL_NSH=}" ;;
    -DFILES_UNINSTALL_NSH=*) test -s "${arg#-DFILES_UNINSTALL_NSH=}" ;;
  esac
done
printf 'FAKE-NSIS-EXE' > "$out"
`
	if err := os.WriteFile(fakeMakensis, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	outExe := filepath.Join(dir, "out", "tisane-setup.exe")
	err := run([]string{
		"-script", scriptPath,
		"-icon", pngPath,
		"-out", outExe,
		"-version", "0.3.0",
		"-makensis", fakeMakensis,
		"-bin", binPath + "=tisane.exe",
		"-file", dictPath + "=dictionaries/english.tsd",
	})
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	gotOut, err := os.ReadFile(outExe)
	if err != nil {
		t.Fatalf("read output exe: %v", err)
	}
	if string(gotOut) != "FAKE-NSIS-EXE" {
		t.Errorf("unexpected output content: %q", string(gotOut))
	}

	logged, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read makensis args log: %v", err)
	}
	logStr := string(logged)
	for _, want := range []string{
		"-V2",
		"-DAPP_VERSION=0.3.0",
		"-DAPP_VERSION_QUAD=0.3.0.0",
		"-DOUT_FILE=",
		scriptPath,
	} {
		if !strings.Contains(logStr, want) {
			t.Errorf("makensis args missing %q:\n%s", want, logStr)
		}
	}
}

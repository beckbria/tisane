package main

import (
	"debug/pe"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/tc-hib/winres"
)

func writeTestPNG(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "icon.png")
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0x2b, G: 0x6c, B: 0xb0, A: 0xff})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create test png: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("encode test png: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close test png: %v", err)
	}
	return p
}

func TestParseVersionQuad(t *testing.T) {
	cases := []struct {
		in   string
		want [4]uint16
	}{
		{"0.3.0", [4]uint16{0, 3, 0, 0}},
		{"1.2.3.4", [4]uint16{1, 2, 3, 4}},
		{"0.3.0-rc1+build", [4]uint16{0, 3, 0, 0}},
		{"", [4]uint16{0, 0, 0, 0}},
	}
	for _, tc := range cases {
		if got := parseVersionQuad(tc.in); got != tc.want {
			t.Errorf("parseVersionQuad(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRunGeneratesValidSyso(t *testing.T) {
	tmp := t.TempDir()
	iconPath := writeTestPNG(t, tmp)
	outPath := filepath.Join(tmp, "rsrc_windows_amd64.syso")

	cfg := config{
		iconPath:    iconPath,
		outPath:     outPath,
		arch:        "amd64",
		appVersion:  "0.3.0",
		productName: "Tisane",
		description: "Tisane test",
	}

	rs, err := buildResourceSet(cfg)
	if err != nil {
		t.Fatalf("buildResourceSet: %v", err)
	}
	if _, err := rs.GetIcon(winres.RT_ICON); err != nil {
		t.Fatalf("expected icon at resource ID 3: %v", err)
	}
	if got := rs.Get(winres.RT_MANIFEST, winres.ID(1), 0x0409); len(got) == 0 {
		t.Fatalf("expected non-empty RT_MANIFEST resource")
	}
	if got := rs.Get(winres.RT_VERSION, winres.ID(1), 0x0409); len(got) == 0 {
		t.Fatalf("expected non-empty RT_VERSION resource")
	}

	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read syso: %v", err)
	}
	if len(data) < 20 {
		t.Fatalf("syso too small: %d bytes", len(data))
	}
	// AMD64 COFF machine header is 0x8664 (little-endian: 0x64, 0x86).
	machine := uint16(data[0]) | uint16(data[1])<<8
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("COFF machine = 0x%x, want 0x%x", machine, pe.IMAGE_FILE_MACHINE_AMD64)
	}
}

func TestRunRejectsUnsupportedArch(t *testing.T) {
	tmp := t.TempDir()
	err := run(config{
		iconPath: writeTestPNG(t, tmp),
		outPath:  filepath.Join(tmp, "out.syso"),
		arch:     "mips",
	})
	if err == nil {
		t.Fatal("expected error for unsupported arch")
	}
}

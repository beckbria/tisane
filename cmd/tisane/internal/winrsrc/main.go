// Command winrsrc generates a Windows COFF resource object (.syso) embedding
// the Tisane multi-resolution icon (at resource ID 3 for Wails v3 and Windows
// Shell / Start Menu), an application manifest, and version metadata.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"tisane/pkg/profile"
)

// iconSizes are the standard Windows 10/11 icon resolutions embedded in the
// PE RT_GROUP_ICON resource (covers All Apps list, Start Menu tiles, Explorer,
// taskbar, and high-DPI scaling).
var iconSizes = []int{256, 128, 64, 48, 32, 24, 16}

type config struct {
	iconPath    string
	outPath     string
	arch        string
	appVersion  string
	productName string
	description string
}

func parseArch(arch string) (winres.Arch, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64":
		return winres.ArchAMD64, nil
	case "arm64":
		return winres.ArchARM64, nil
	case "386":
		return winres.ArchI386, nil
	default:
		return "", fmt.Errorf("unsupported Windows arch %q (expected amd64, arm64, or 386)", arch)
	}
}

func parseVersionQuad(ver string) [4]uint16 {
	var quad [4]uint16
	// Strip any pre-release/build metadata (e.g. "0.3.0-rc1+build").
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
	return quad
}

func buildResourceSet(cfg config) (*winres.ResourceSet, error) {
	if cfg.iconPath == "" {
		return nil, fmt.Errorf("-icon path is required")
	}

	f, err := os.Open(cfg.iconPath)
	if err != nil {
		return nil, fmt.Errorf("open icon %q: %w", cfg.iconPath, err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode PNG %q: %w", cfg.iconPath, err)
	}

	ico, err := winres.NewIconFromResizedImage(img, iconSizes)
	if err != nil {
		return nil, fmt.Errorf("resize icon: %w", err)
	}

	rs := &winres.ResourceSet{}

	// Wails v3 looks up the embedded application icon at resource ID 3
	// (winres.RT_ICON == winres.ID(3)). Because it is the only RT_GROUP_ICON
	// resource in the PE binary, Windows Explorer and the Windows 10/11 Start
	// Menu also pick it automatically.
	if err := rs.SetIcon(winres.RT_ICON, ico); err != nil {
		return nil, fmt.Errorf("set icon resource: %w", err)
	}

	quad := parseVersionQuad(cfg.appVersion)
	rs.SetManifest(winres.AppManifest{
		Identity: winres.AssemblyIdentity{
			Name:    cfg.productName,
			Version: quad,
		},
		Description:         cfg.description,
		Compatibility:       winres.Win7AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	var vi version.Info
	vi.FileVersion = quad
	vi.ProductVersion = quad
	vi.Type = version.App
	for k, v := range map[string]string{
		version.ProductName:      cfg.productName,
		version.FileDescription:  cfg.description,
		version.InternalName:     "tisane",
		version.OriginalFilename: "tisane.exe",
		version.FileVersion:      cfg.appVersion,
		version.ProductVersion:   cfg.appVersion,
	} {
		if err := vi.Set(version.LangDefault, k, v); err != nil {
			return nil, fmt.Errorf("set version field %q: %w", k, err)
		}
	}
	rs.SetVersionInfo(vi)

	return rs, nil
}

func run(cfg config) error {
	if cfg.outPath == "" {
		return fmt.Errorf("-out path is required")
	}
	targetArch, err := parseArch(cfg.arch)
	if err != nil {
		return err
	}

	rs, err := buildResourceSet(cfg)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(cfg.outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory %q: %w", dir, err)
		}
	}

	out, err := os.Create(cfg.outPath)
	if err != nil {
		return fmt.Errorf("create output %q: %w", cfg.outPath, err)
	}
	if err := rs.WriteObject(out, targetArch); err != nil {
		_ = out.Close()
		return fmt.Errorf("write COFF object %q: %w", cfg.outPath, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output %q: %w", cfg.outPath, err)
	}
	return nil
}

func main() {
	var cfg config
	flag.StringVar(&cfg.iconPath, "icon", "", "Path to source PNG icon")
	flag.StringVar(&cfg.outPath, "out", "", "Path to output .syso COFF file")
	flag.StringVar(&cfg.arch, "arch", "amd64", "Target Windows architecture (amd64, arm64, 386)")
	flag.StringVar(&cfg.appVersion, "version", profile.Version, "Application version string")
	flag.StringVar(&cfg.productName, "name", "Tisane", "Application product name")
	flag.StringVar(&cfg.description, "desc", "High-speed crossword, anagram, and wordplay dictionary search", "Application description")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "winrsrc: %v\n", err)
		os.Exit(1)
	}
}

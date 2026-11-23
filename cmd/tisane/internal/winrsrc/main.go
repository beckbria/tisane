// Command winrsrc generates a Windows COFF resource object (.syso) embedding
// the Tisane multi-resolution icon (at resource ID 3 for Wails v3 and Windows
// Shell / Start Menu), an application manifest, and version metadata.
package main

import (
	"encoding/json"
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
	iconPath     string
	manifestPath string
	infoPath     string
	outPath      string
	arch         string
	appVersion   string
	productName  string
	description  string
}

// winInfoFile matches Wails v3's build/windows/info.json layout.
type winInfoFile struct {
	Fixed struct {
		FileVersion    string `json:"file_version"`
		ProductVersion string `json:"product_version"`
	} `json:"fixed"`
	Info map[string]map[string]string `json:"info"`
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
	if cfg.manifestPath != "" {
		manifestData, err := os.ReadFile(cfg.manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read manifest %q: %w", cfg.manifestPath, err)
		}
		if len(strings.TrimSpace(string(manifestData))) == 0 {
			return nil, fmt.Errorf("manifest %q is empty", cfg.manifestPath)
		}
		if err := rs.Set(winres.RT_MANIFEST, winres.ID(1), version.LangDefault, manifestData); err != nil {
			return nil, fmt.Errorf("set manifest resource: %w", err)
		}
	} else {
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
	}

	vi, err := buildVersionInfo(cfg, quad)
	if err != nil {
		return nil, err
	}
	rs.SetVersionInfo(vi)

	return rs, nil
}

func buildVersionInfo(cfg config, defaultQuad [4]uint16) (version.Info, error) {
	var vi version.Info
	vi.FileVersion = defaultQuad
	vi.ProductVersion = defaultQuad
	vi.Type = version.App

	if cfg.infoPath == "" {
		for k, v := range map[string]string{
			version.ProductName:      cfg.productName,
			version.FileDescription:  cfg.description,
			version.InternalName:     "tisane",
			version.OriginalFilename: "tisane.exe",
			version.FileVersion:      cfg.appVersion,
			version.ProductVersion:   cfg.appVersion,
		} {
			if err := vi.Set(version.LangDefault, k, v); err != nil {
				return vi, fmt.Errorf("set version field %q: %w", k, err)
			}
		}
		return vi, nil
	}

	raw, err := os.ReadFile(cfg.infoPath)
	if err != nil {
		return vi, fmt.Errorf("read info %q: %w", cfg.infoPath, err)
	}
	var info winInfoFile
	if err := json.Unmarshal(raw, &info); err != nil {
		return vi, fmt.Errorf("parse info %q: %w", cfg.infoPath, err)
	}

	if v := strings.TrimSpace(info.Fixed.FileVersion); v != "" {
		vi.FileVersion = parseVersionQuad(v)
	}
	if v := strings.TrimSpace(info.Fixed.ProductVersion); v != "" {
		vi.ProductVersion = parseVersionQuad(v)
	} else if v := strings.TrimSpace(info.Fixed.FileVersion); v != "" {
		vi.ProductVersion = parseVersionQuad(v)
	}

	if len(info.Info) == 0 {
		info.Info = map[string]map[string]string{"0000": {}}
	}
	for langHex, fields := range info.Info {
		langID := uint16(version.LangDefault)
		if trimmed := strings.TrimSpace(langHex); trimmed != "" && trimmed != "0000" {
			parsed, err := strconv.ParseUint(trimmed, 16, 16)
			if err != nil {
				return vi, fmt.Errorf("invalid language ID %q in %q: %w", langHex, cfg.infoPath, err)
			}
			langID = uint16(parsed)
		}
		for k, v := range fields {
			if strings.TrimSpace(v) == "" {
				switch k {
				case version.FileVersion, version.ProductVersion:
					v = cfg.appVersion
				default:
					continue
				}
			}
			if err := vi.Set(langID, k, v); err != nil {
				return vi, fmt.Errorf("set version field %q: %w", k, err)
			}
		}
		if _, ok := fields[version.FileVersion]; !ok && cfg.appVersion != "" {
			if err := vi.Set(langID, version.FileVersion, cfg.appVersion); err != nil {
				return vi, fmt.Errorf("set default FileVersion: %w", err)
			}
		}
		if _, ok := fields[version.ProductVersion]; !ok && cfg.appVersion != "" {
			if err := vi.Set(langID, version.ProductVersion, cfg.appVersion); err != nil {
				return vi, fmt.Errorf("set default ProductVersion: %w", err)
			}
		}
	}
	return vi, nil
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
	flag.StringVar(&cfg.manifestPath, "manifest", "", "Optional path to Windows application manifest XML")
	flag.StringVar(&cfg.infoPath, "info", "", "Optional path to Windows version metadata JSON (Wails info.json format)")
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

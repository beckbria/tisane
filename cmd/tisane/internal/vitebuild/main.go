// Command vitebuild is a cross-platform Bazel build helper that stages the
// Svelte 5 frontend into a writable temporary directory, installs or links
// node_modules, runs Vite, and copies the compiled bundle into Bazel's output
// directory without relying on bash or POSIX shell utilities.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <package.json> <out-dist-dir>\n", os.Args[0])
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "vitebuild: %v\n", err)
		os.Exit(1)
	}
}

func run(pkgJSONArg, outDistArg string) error {
	absPkgJSON, err := filepath.Abs(filepath.FromSlash(pkgJSONArg))
	if err != nil {
		return fmt.Errorf("resolve package.json path: %w", err)
	}
	absOutDist, err := filepath.Abs(filepath.FromSlash(outDistArg))
	if err != nil {
		return fmt.Errorf("resolve output directory path: %w", err)
	}

	frontendSrcDir := filepath.Dir(absPkgJSON)

	var realNodeModules string
	if realPkgJSON, err := filepath.EvalSymlinks(absPkgJSON); err == nil {
		candidate := filepath.Join(filepath.Dir(realPkgJSON), "node_modules")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			realNodeModules = candidate
		}
	}

	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node executable not found in PATH (Node.js 20+ required): %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "tv-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	linkedNodeModules := filepath.Join(tmpDir, "node_modules")
	usedLink := false
	defer func() {
		if usedLink {
			_ = os.Remove(linkedNodeModules)
		}
		_ = os.RemoveAll(tmpDir)
	}()

	skipTopLevel := map[string]bool{
		"node_modules": true,
		"dist":         true,
	}
	if err := copyDir(frontendSrcDir, tmpDir, skipTopLevel); err != nil {
		return fmt.Errorf("stage frontend sources: %w", err)
	}

	if realNodeModules != "" {
		if err := linkDir(realNodeModules, linkedNodeModules); err == nil {
			usedLink = true
		}
	}
	if !usedLink {
		if err := npmCI(tmpDir); err != nil {
			return fmt.Errorf("npm ci failed: %w", err)
		}
	}

	if err := viteBuild(nodeBin, tmpDir); err != nil {
		if !usedLink {
			return fmt.Errorf("vite build failed: %w", err)
		}
		// Workspace node_modules may have been installed for another OS (e.g. WSL vs Windows).
		_ = os.Remove(linkedNodeModules)
		usedLink = false
		if err := npmCI(tmpDir); err != nil {
			return fmt.Errorf("npm ci fallback failed: %w", err)
		}
		if err := viteBuild(nodeBin, tmpDir); err != nil {
			return fmt.Errorf("vite build failed: %w", err)
		}
	}

	builtDist := filepath.Join(tmpDir, "dist")
	if err := clearDir(absOutDist); err != nil {
		return fmt.Errorf("prepare output dir: %w", err)
	}
	if err := copyDir(builtDist, absOutDist, nil); err != nil {
		return fmt.Errorf("copy dist output: %w", err)
	}
	return nil
}

func npmCI(dir string) error {
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm executable not found in PATH: %w", err)
	}
	return runCmd(dir, npmBin, "ci", "--no-audit", "--no-fund")
}

func viteBuild(nodeBin, dir string) error {
	viteJS := filepath.Join(dir, "node_modules", "vite", "bin", "vite.js")
	if _, err := os.Stat(viteJS); err == nil {
		return runCmd(dir, nodeBin, viteJS, "build")
	}
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm executable not found in PATH: %w", err)
	}
	return runCmd(dir, npmBin, "run", "build")
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "npm_config_update_notifier=false")
	return cmd.Run()
}

func linkDir(target, linkPath string) error {
	if err := os.Symlink(target, linkPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	// On Windows without Developer Mode, directory symlinks require elevation,
	// whereas NTFS directory junctions (mklink /J) work unprivileged.
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", linkPath, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J: %w (%s)", err, string(out))
	}
	return nil
}

func clearDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(srcDir, dstDir string, skip map[string]bool) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if skip[entry.Name()] {
			continue
		}
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		info, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath, nil); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
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

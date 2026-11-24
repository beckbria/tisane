// Command vitebuild is a cross-platform Bazel build helper that stages the
// Svelte 5 frontend into a writable temporary directory, installs or links
// node_modules, runs Vite, and copies the compiled bundle into Bazel's output
// directory without relying on bash or POSIX shell utilities.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"tisane"
)

func main() {
	fs := flag.NewFlagSet("vitebuild", flag.ContinueOnError)
	nodeFlag := fs.String("node", "", "Optional path to hermetic node executable")
	if err := fs.Parse(os.Args[1:]); err != nil || fs.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [-node <node-bin>] <package.json> <out-dist-dir>\n", os.Args[0])
		os.Exit(2)
	}
	if err := run(*nodeFlag, fs.Arg(0), fs.Arg(1)); err != nil {
		fmt.Fprintf(os.Stderr, "vitebuild: %v\n", err)
		os.Exit(1)
	}
}

func resolveNodeAndNPM(nodeFlag string) (nodeBin string, nodeDir string, err error) {
	if strings.TrimSpace(nodeFlag) != "" {
		absNode, absErr := filepath.Abs(filepath.FromSlash(nodeFlag))
		if absErr != nil {
			return "", "", fmt.Errorf("resolve -node path %q: %w", nodeFlag, absErr)
		}
		if _, statErr := os.Stat(absNode); statErr != nil {
			return "", "", fmt.Errorf("hermetic node binary %q not found: %w", absNode, statErr)
		}
		return absNode, filepath.Dir(absNode), nil
	}
	found, lookErr := exec.LookPath("node")
	if lookErr != nil {
		return "", "", fmt.Errorf("node executable not found in PATH (Node.js 20+ required): %w", lookErr)
	}
	return found, filepath.Dir(found), nil
}

func resolveNPM(nodeDir string) (string, error) {
	if nodeDir != "" {
		names := []string{"npm"}
		if runtime.GOOS == "windows" {
			names = []string{"npm.cmd", "npm.exe", "npm"}
		}
		for _, name := range names {
			cand := filepath.Join(nodeDir, name)
			if info, err := os.Stat(cand); err == nil && !info.IsDir() {
				return cand, nil
			}
		}
	}
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		return "", fmt.Errorf("npm executable not found in PATH: %w", err)
	}
	return npmBin, nil
}

func run(nodeFlag, pkgJSONArg, outDistArg string) error {
	absPkgJSON, err := filepath.Abs(filepath.FromSlash(pkgJSONArg))
	if err != nil {
		return fmt.Errorf("resolve package.json path: %w", err)
	}
	absOutDist, err := filepath.Abs(filepath.FromSlash(outDistArg))
	if err != nil {
		return fmt.Errorf("resolve output directory path: %w", err)
	}
	nodeBin, nodeDir, err := resolveNodeAndNPM(nodeFlag)
	if err != nil {
		return err
	}

	frontendSrcDir := filepath.Dir(absPkgJSON)

	var realNodeModules string
	if realPkgJSON, err := filepath.EvalSymlinks(absPkgJSON); err == nil {
		candidate := filepath.Join(filepath.Dir(realPkgJSON), "node_modules")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			realNodeModules = candidate
		}
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
		if err := npmCI(nodeDir, tmpDir); err != nil {
			return fmt.Errorf("npm ci failed: %w", err)
		}
	}

	if err := viteBuild(nodeBin, nodeDir, tmpDir); err != nil {
		if !usedLink {
			return fmt.Errorf("vite build failed: %w", err)
		}
		// Workspace node_modules may have been installed for another OS (e.g. WSL vs Windows).
		// Never run npm ci if unlinking fails, or npm ci could mutate the workspace node_modules.
		if rmErr := os.Remove(linkedNodeModules); rmErr != nil {
			return fmt.Errorf("vite build failed (%v) and unlinking staged node_modules failed: %w", err, rmErr)
		}
		usedLink = false
		if err := npmCI(nodeDir, tmpDir); err != nil {
			return fmt.Errorf("npm ci fallback failed: %w", err)
		}
		if err := viteBuild(nodeBin, nodeDir, tmpDir); err != nil {
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

func npmCI(nodeDir, dir string) error {
	npmBin, err := resolveNPM(nodeDir)
	if err != nil {
		return err
	}
	return runCmd(nodeDir, dir, npmBin, "ci", "--no-audit", "--no-fund")
}

func viteBuild(nodeBin, nodeDir, dir string) error {
	viteJS := filepath.Join(dir, "node_modules", "vite", "bin", "vite.js")
	if _, err := os.Stat(viteJS); err == nil {
		return runCmd(nodeDir, dir, nodeBin, viteJS, "build")
	}
	npmBin, err := resolveNPM(nodeDir)
	if err != nil {
		return err
	}
	return runCmd(nodeDir, dir, npmBin, "run", "build")
}

func runCmd(nodeDir, dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	env := os.Environ()
	if nodeDir != "" {
		env = append(env, "PATH="+nodeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	env = append(env,
		"npm_config_update_notifier=false",
		"TISANE_VERSION="+tisane.Version,
	)
	cmd.Env = env
	return cmd.Run()
}

func linkDir(target, linkPath string) error {
	if err := os.Symlink(target, linkPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	if strings.ContainsAny(target, "&|<>^%!\"\r\n") || strings.ContainsAny(linkPath, "&|<>^%!\"\r\n") {
		return fmt.Errorf("unsafe shell characters in Windows junction path")
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

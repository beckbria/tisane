//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func openDirectoryInFileManager(cleanDir string) error {
	if strings.ContainsRune(cleanDir, '"') {
		return fmt.Errorf("invalid directory path %q", cleanDir)
	}
	cmd := exec.Command("explorer.exe")
	// Pass an explicitly quoted path so Windows Explorer treats paths containing spaces or commas as a single folder.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(`explorer.exe "%s"`, cleanDir),
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

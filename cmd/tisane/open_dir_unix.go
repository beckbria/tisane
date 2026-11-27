//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func openDirectoryInFileManager(cleanDir string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", cleanDir)
	} else {
		cmd = exec.Command("xdg-open", cleanDir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

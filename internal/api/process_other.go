//go:build !windows

package api

import "os/exec"

func hideCommandWindow(command *exec.Cmd) {}

//go:build !windows

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func defaultSignalProcess(pid int, sig os.Signal) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported signal %v", sig)
	}
	return syscall.Kill(pid, sysSig)
}

func defaultProcessAlive(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

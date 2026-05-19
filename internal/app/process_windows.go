//go:build windows

package app

import (
	"errors"
	"os"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	errorInvalidParameter          = syscall.Errno(87)
)

func defaultSignalProcess(pid int, sig os.Signal) error {
	if sig != os.Kill && sig != syscall.SIGTERM && sig != syscall.SIGKILL {
		return syscall.EWINDOWS
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func defaultProcessAlive(pid int) (bool, error) {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if errors.Is(err, errorInvalidParameter) {
			return false, nil
		}
		return false, err
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, err
	}
	return exitCode == stillActive, nil
}

package app

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	automaticMihomoInterfaceName = "auto"
	defaultInterfaceLookupTime   = 3 * time.Second
)

var lookupDefaultPhysicalInterface = defaultPhysicalInterface

func resolveMihomoInterfaceName(ctx context.Context, configured string) (string, error) {
	interfaceName := strings.TrimSpace(configured)
	if interfaceName == "" || !strings.EqualFold(interfaceName, automaticMihomoInterfaceName) {
		return interfaceName, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, defaultInterfaceLookupTime)
	defer cancel()

	interfaceName, err := lookupDefaultPhysicalInterface(lookupCtx)
	if err != nil {
		return "", fmt.Errorf("resolve automatic mihomo interface: %w", err)
	}
	interfaceName = strings.TrimSpace(interfaceName)
	if interfaceName == "" {
		return "", fmt.Errorf("resolve automatic mihomo interface: default route has no physical interface")
	}
	return interfaceName, nil
}

func defaultPhysicalInterface(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("interface_name %q is only supported on macOS", automaticMihomoInterfaceName)
	}

	output, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return "", fmt.Errorf("query default route: %w", err)
	}
	interfaceName, err := parseDefaultRouteInterface(string(output))
	if err != nil {
		return "", err
	}

	device, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return "", fmt.Errorf("inspect default route interface %q: %w", interfaceName, err)
	}
	if device.Flags&net.FlagUp == 0 {
		return "", fmt.Errorf("default route interface %q is not up", interfaceName)
	}
	if device.Flags&net.FlagLoopback != 0 || len(device.HardwareAddr) == 0 {
		return "", fmt.Errorf("default route interface %q is not a physical network interface", interfaceName)
	}
	return interfaceName, nil
}

func parseDefaultRouteInterface(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "interface:" {
			if interfaceName := strings.TrimSpace(fields[1]); interfaceName != "" {
				return interfaceName, nil
			}
		}
	}
	return "", fmt.Errorf("default route output does not contain an interface")
}

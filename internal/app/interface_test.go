package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveMihomoInterfaceNamePreservesUnsetAndFixedValues(t *testing.T) {
	oldLookup := lookupDefaultPhysicalInterface
	t.Cleanup(func() { lookupDefaultPhysicalInterface = oldLookup })
	lookupDefaultPhysicalInterface = func(context.Context) (string, error) {
		t.Fatal("lookup should not run for an unset or fixed interface")
		return "", nil
	}

	for _, tc := range []struct {
		configured string
		want       string
	}{
		{configured: "", want: ""},
		{configured: "  en7  ", want: "en7"},
	} {
		got, err := resolveMihomoInterfaceName(context.Background(), tc.configured)
		if err != nil {
			t.Fatalf("resolveMihomoInterfaceName(%q) returned error: %v", tc.configured, err)
		}
		if got != tc.want {
			t.Fatalf("resolveMihomoInterfaceName(%q) = %q, want %q", tc.configured, got, tc.want)
		}
	}
}

func TestResolveMihomoInterfaceNameAutoUsesCurrentPhysicalInterface(t *testing.T) {
	oldLookup := lookupDefaultPhysicalInterface
	t.Cleanup(func() { lookupDefaultPhysicalInterface = oldLookup })
	lookupDefaultPhysicalInterface = func(context.Context) (string, error) {
		return "en12", nil
	}

	got, err := resolveMihomoInterfaceName(context.Background(), " AUTO ")
	if err != nil {
		t.Fatalf("resolveMihomoInterfaceName() returned error: %v", err)
	}
	if got != "en12" {
		t.Fatalf("resolveMihomoInterfaceName() = %q, want en12", got)
	}
}

func TestResolveMihomoInterfaceNameAutoReportsLookupFailure(t *testing.T) {
	oldLookup := lookupDefaultPhysicalInterface
	t.Cleanup(func() { lookupDefaultPhysicalInterface = oldLookup })
	lookupDefaultPhysicalInterface = func(context.Context) (string, error) {
		return "", errors.New("no default route")
	}

	_, err := resolveMihomoInterfaceName(context.Background(), "auto")
	if err == nil || !strings.Contains(err.Error(), "resolve automatic mihomo interface") {
		t.Fatalf("resolveMihomoInterfaceName() error = %v, want automatic interface context", err)
	}
}

func TestParseDefaultRouteInterface(t *testing.T) {
	got, err := parseDefaultRouteInterface(`   route to: default
destination: default
  interface: en10
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`)
	if err != nil {
		t.Fatalf("parseDefaultRouteInterface() returned error: %v", err)
	}
	if got != "en10" {
		t.Fatalf("parseDefaultRouteInterface() = %q, want en10", got)
	}

	if _, err := parseDefaultRouteInterface("route to: default\n"); err == nil {
		t.Fatal("parseDefaultRouteInterface() error = nil, want missing interface error")
	}
}

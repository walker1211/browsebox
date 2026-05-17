package mihomo

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRewriteConfigForIsolatedRuntime(t *testing.T) {
	input := strings.Join([]string{
		"mixed-port: 7890",
		"allow-lan: true",
		"bind-address: 0.0.0.0",
		"external-controller: 0.0.0.0:9090",
		"tun:",
		"  enable: true",
		"  stack: system",
		"proxies:",
		"  - name: node-a",
		"    server: example.com",
		"",
	}, "\n")

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17890, ControllerPort: 19090})

	assertContains(t, got, "mixed-port: 17890\n")
	assertContains(t, got, "allow-lan: false\n")
	assertContains(t, got, "bind-address: 127.0.0.1\n")
	assertContains(t, got, "external-controller: 127.0.0.1:19090\n")
	assertContains(t, got, "tun:\n  enable: false\n  stack: system\n")
	assertNotContains(t, got, "mixed-port: 7890")
	assertNotContains(t, got, "allow-lan: true")
	assertNotContains(t, got, "bind-address: 0.0.0.0")
	assertNotContains(t, got, "external-controller: 0.0.0.0:9090")
	assertNotContains(t, got, "enable: true")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigRemovesUnsafeTopLevelListenerControllerAndSecretKeys(t *testing.T) {
	input := strings.Join([]string{
		"port: 7890",
		"socks-port: 7891",
		"redir-port: 7892",
		"tproxy-port: 7893",
		"external-controller-tls: 0.0.0.0:9443",
		"secret: super-secret-token",
		"proxies:",
		"  - name: credentialed-node",
		"    type: ss",
		"    server: example.com",
		"    port: 443",
		"    password: proxy-password",
		"",
	}, "\n")

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17890, ControllerPort: 19090})

	for _, unsafe := range []string{
		"\nport: 7890\n",
		"\nsocks-port: 7891\n",
		"\nredir-port: 7892\n",
		"\ntproxy-port: 7893\n",
		"\nexternal-controller-tls: 0.0.0.0:9443\n",
		"\nsecret: super-secret-token\n",
	} {
		assertNotContains(t, "\n"+got, unsafe)
	}
	assertContains(t, got, "mixed-port: 17890\n")
	assertContains(t, got, "external-controller: 127.0.0.1:19090\n")
	assertContains(t, got, "proxies:\n  - name: credentialed-node\n    type: ss\n    server: example.com\n    port: 443\n    password: proxy-password\n")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigAddsMissingControllerBindAndTun(t *testing.T) {
	input := "proxies:\n  - name: node-a\n    type: ss\n"

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17891, ControllerPort: 19091})

	assertContains(t, got, "mixed-port: 17891\n")
	assertContains(t, got, "allow-lan: false\n")
	assertContains(t, got, "bind-address: 127.0.0.1\n")
	assertContains(t, got, "external-controller: 127.0.0.1:19091\n")
	assertContains(t, got, "tun:\n  enable: false\n")
	assertContains(t, got, "proxies:\n  - name: node-a\n")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigAddsMissingTunEnableInsideExistingTunBlock(t *testing.T) {
	input := strings.Join([]string{
		"tun:",
		"    stack: system",
		"    auto-route: true",
		"proxies:",
		"  - name: node-a",
		"",
	}, "\n")

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17892, ControllerPort: 19092})

	assertContains(t, got, "tun:\n    enable: false\n    stack: system\n    auto-route: true\nproxies:")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigIgnoresNestedTunEnable(t *testing.T) {
	input := strings.Join([]string{
		"tun:",
		"  device:",
		"    enable: true",
		"  stack: system",
		"proxies:",
		"  - name: node-a",
		"",
	}, "\n")

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17898, ControllerPort: 19098})

	assertContains(t, got, "tun:\n  enable: false\n  device:\n    enable: true\n  stack: system\nproxies:")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigReplacesInlineTunValuesWithDisabledBlock(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "flow", input: "tun: { enable: true }\nproxies: []\n"},
		{name: "scalar", input: "tun: true\nproxies: []\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteConfig(tc.input, RuntimeConfigOptions{ProxyPort: 17899, ControllerPort: 19099})

			assertContains(t, got, "tun:\n  enable: false\nproxies: []\n")
			assertNotContains(t, got, "tun: { enable: true }")
			assertNotContains(t, got, "tun: true")
			assertSingleTrailingNewline(t, got)
		})
	}
}

func TestRewriteConfigTreatsTunInlineCommentAsBlockStyle(t *testing.T) {
	input := strings.Join([]string{
		"tun: # preserve block-style comment",
		"  enable: true",
		"  stack: system",
		"proxies: []",
		"",
	}, "\n")

	got := RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17900, ControllerPort: 19100})

	assertContains(t, got, "tun: # preserve block-style comment\n  enable: false\n  stack: system\nproxies: []\n")
	assertNotContains(t, got, "enable: true")
	assertSingleTrailingNewline(t, got)
}

func TestRewriteConfigForcesProxyGroupSelectionHint(t *testing.T) {
	got := RewriteConfig("proxies: []\n", RuntimeConfigOptions{
		ProxyPort:      17893,
		ControllerPort: 19093,
		Group:          "AUTO",
		Node:           "node-a",
	})

	assertContains(t, got, "# browsebox group: AUTO\n")
	assertContains(t, got, "# browsebox node: node-a\n")
}

func TestRewriteConfigEscapesSelectionHintControlCharacters(t *testing.T) {
	got := RewriteConfig("proxies: []\n", RuntimeConfigOptions{
		ProxyPort:      17897,
		ControllerPort: 19097,
		Group:          "AUTO\nallow-lan: true",
		Node:           "node-a\r\nexternal-controller: 0.0.0.0:9090",
	})

	assertContains(t, got, "# browsebox group: AUTO\\nallow-lan: true\n")
	assertContains(t, got, "# browsebox node: node-a\\r\\nexternal-controller: 0.0.0.0:9090\n")
	assertNotContains(t, got, "\nallow-lan: true\n")
	assertNotContains(t, got, "\nexternal-controller: 0.0.0.0:9090\n")

	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if strings.Contains(line, "browsebox group:") || strings.Contains(line, "browsebox node:") {
			if !strings.HasPrefix(line, "# ") {
				t.Fatalf("selection hint marker is not a comment line: %q", line)
			}
		}
	}
}

func TestRewriteConfigOmitsSelectionHintWhenIncomplete(t *testing.T) {
	got := RewriteConfig("proxies: []\n", RuntimeConfigOptions{
		ProxyPort:      17894,
		ControllerPort: 19094,
		Group:          "AUTO",
	})

	assertNotContains(t, got, "# browsebox group:")
	assertNotContains(t, got, "# browsebox node:")
}

func TestRewriteConfigDoesNotMutateInput(t *testing.T) {
	input := "mixed-port: 7890\nallow-lan: true\n"
	original := input

	_ = RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17895, ControllerPort: 19095})

	if input != original {
		t.Fatal("RewriteConfig mutated input string")
	}
}

func TestRewriteConfigRetainsProxyDefinitionsWithoutLogging(t *testing.T) {
	secret := "fake-secret-password"
	input := "proxies:\n  - name: secret-node\n    type: ss\n    password: " + secret + "\n"

	var got string
	output := captureOutput(t, func() {
		got = RewriteConfig(input, RuntimeConfigOptions{ProxyPort: 17896, ControllerPort: 19096})
	})

	if !strings.Contains(got, "password: "+secret) {
		t.Fatal("rewritten config did not retain proxy password")
	}
	if output != "" {
		t.Fatal("RewriteConfig wrote to stdout or stderr")
	}
}

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	stdoutBytes, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	stderrBytes, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	_ = stdoutReader.Close()
	_ = stderrReader.Close()

	return string(stdoutBytes) + string(stderrBytes)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("rewritten config missing expected fragment %q", want)
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Fatalf("rewritten config contains unsafe fragment %q", unwanted)
	}
}

func assertSingleTrailingNewline(t *testing.T, got string) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") {
		t.Fatal("rewritten config does not end with a trailing newline")
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatal("rewritten config ends with more than one trailing newline")
	}
}

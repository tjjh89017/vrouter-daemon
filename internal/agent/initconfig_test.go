package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInitConfigYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.yaml")

	content := `config: |
  interfaces {
    ethernet eth0 {
      address dhcp
    }
  }
commands: |
  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
  set firewall name MGMT rule 10 action accept
`
	os.WriteFile(path, []byte(content), 0644)

	ic, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig error: %v", err)
	}

	if !strings.Contains(ic.Config, "ethernet eth0") {
		t.Fatalf("expected config to contain 'ethernet eth0', got: %s", ic.Config)
	}
	if !strings.Contains(ic.Commands, "set protocols static route") {
		t.Fatalf("expected commands to contain 'set protocols static route', got: %s", ic.Commands)
	}
	if ic.IsEmpty() {
		t.Fatal("expected non-empty init config")
	}
}

func TestLoadInitConfigEmpty(t *testing.T) {
	ic, err := LoadInitConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ic.IsEmpty() {
		t.Fatal("expected empty init config")
	}
}

func TestLoadInitConfigOnlyCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.yaml")

	content := `commands: |
  set interfaces ethernet eth0 address dhcp
`
	os.WriteFile(path, []byte(content), 0644)

	ic, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig error: %v", err)
	}
	if ic.Config != "" {
		t.Fatalf("expected empty config, got: %s", ic.Config)
	}
	if ic.IsEmpty() {
		t.Fatal("expected non-empty init config")
	}
}

// --- RenderScript (failover, init config only) ---

func TestRenderScript(t *testing.T) {
	ic := &InitConfig{
		Config:   "interfaces {\n  ethernet eth0 {\n    address dhcp\n  }\n}",
		Commands: "set protocols static route 0.0.0.0/0 next-hop 192.168.1.1",
	}

	script, err := ic.RenderScript()
	if err != nil {
		t.Fatalf("RenderScript error: %v", err)
	}

	s := string(script)
	assertContains(t, s, "#!/bin/vbash")
	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "ethernet eth0")
	assertContains(t, s, "set protocols static route")
	assertContains(t, s, "commit")
	assertContains(t, s, "save")
	// No merge — failover only uses init config
	assertNotContains(t, s, "merge")
}

func TestRenderScriptNoConfig(t *testing.T) {
	ic := &InitConfig{
		Commands: "set interfaces ethernet eth0 address dhcp",
	}

	script, err := ic.RenderScript()
	s := mustRenderBytes(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	assertContains(t, s, "set interfaces ethernet eth0")
}

func TestRenderScriptEmpty(t *testing.T) {
	ic := &InitConfig{}
	script, err := ic.RenderScript()
	s := mustRenderBytes(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
}

// --- RenderMergedScript (server push with init config) ---

func TestRenderMergedScript_InitConfigAndPushedConfig(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { base config }",
		Commands: "set init-cmd",
	}

	script, err := ic.RenderMergedScript("pushed { overlay config }", "set pushed-cmd")
	s := mustRenderBytes(t, script, err)

	// 1. load init config
	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "init { base config }")
	// 2. init commands
	assertContains(t, s, "set init-cmd")
	// 3. merge pushed config from file
	assertContains(t, s, "merge /tmp/vrouter-pushed.config")
	// 4. pushed commands
	assertContains(t, s, "set pushed-cmd")

	// Verify ordering: init commands BEFORE merge BEFORE pushed commands
	initCmdIdx := strings.Index(s, "set init-cmd")
	mergeIdx := strings.Index(s, "merge /tmp/vrouter-pushed.config")
	pushedCmdIdx := strings.Index(s, "set pushed-cmd")

	if initCmdIdx > mergeIdx {
		t.Fatal("init commands must come before merge")
	}
	if mergeIdx > pushedCmdIdx {
		t.Fatal("merge must come before pushed commands")
	}
}

func TestRenderMergedScript_InitCommandsOnly(t *testing.T) {
	ic := &InitConfig{
		Commands: "set init-cmd",
	}

	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRenderBytes(t, script, err)

	// No init config block → pushed config loaded directly
	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "pushed { config }")
	// Init commands still run first
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")
	// No merge needed (no init config block)
	assertNotContains(t, s, "merge")

	initIdx := strings.Index(s, "set init-cmd")
	pushIdx := strings.Index(s, "set pushed-cmd")
	if initIdx > pushIdx {
		t.Fatal("init commands must come before pushed commands")
	}
}

func TestRenderMergedScript_NoPushedConfig(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { config }",
		Commands: "set init-cmd",
	}

	script, err := ic.RenderMergedScript("", "set pushed-cmd")
	s := mustRenderBytes(t, script, err)

	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")
	// No merge — no pushed config block
	assertNotContains(t, s, "merge")
}

func TestRenderMergedScript_NoPushedAnything(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { config }",
		Commands: "set init-cmd",
	}

	script, err := ic.RenderMergedScript("", "")
	s := mustRenderBytes(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertNotContains(t, s, "merge")
	assertNotContains(t, s, "pushed")
}

// --- RenderSimpleScript (no init config) ---

func TestRenderSimpleScript(t *testing.T) {
	script, err := RenderSimpleScript("my { config }", "set my-cmd")
	s := mustRenderBytes(t, script, err)

	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "my { config }")
	assertContains(t, s, "set my-cmd")
	assertNotContains(t, s, "merge")
	assertNotContains(t, s, "init config")
}

func TestRenderSimpleScriptNoConfig(t *testing.T) {
	script, err := RenderSimpleScript("", "set cmd")
	s := mustRenderBytes(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	assertContains(t, s, "set cmd")
}

// --- helpers ---

func mustRenderBytes(t *testing.T, script []byte, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	return string(script)
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected script to contain %q, got:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected script NOT to contain %q, got:\n%s", substr, s)
	}
}

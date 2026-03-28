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
	assertContains(t, ic.Config, "ethernet eth0")
	assertContains(t, ic.Commands, "set protocols static route")
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
	os.WriteFile(path, []byte("commands: |\n  set interfaces ethernet eth0 address dhcp\n"), 0644)

	ic, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig error: %v", err)
	}
	if ic.Config != "" {
		t.Fatalf("expected empty config, got: %s", ic.Config)
	}
	if ic.IsEmpty() {
		t.Fatal("expected non-empty")
	}
}

// --- RenderScript (failover, init config only) ---

func TestRenderScript(t *testing.T) {
	ic := &InitConfig{
		Config:   "interfaces {\n  ethernet eth0 {\n    address dhcp\n  }\n}",
		Commands: "set protocols static route 0.0.0.0/0 next-hop 192.168.1.1",
	}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)

	assertContains(t, s, "#!/bin/vbash")
	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "ethernet eth0")
	assertContains(t, s, "set protocols static route")
	assertContains(t, s, "commit")
	assertContains(t, s, "save")
	assertNotContains(t, s, "merge")
}

func TestRenderScriptNoConfig(t *testing.T) {
	ic := &InitConfig{Commands: "set interfaces ethernet eth0 address dhcp"}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	assertContains(t, s, "set interfaces ethernet eth0")
}

func TestRenderScriptEmpty(t *testing.T) {
	ic := &InitConfig{}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
}

// --- RenderMergedScript (server push + init config) ---

func TestRenderMergedScript_InitConfigAndPushedConfig(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { base config }",
		Commands: "set init-cmd",
	}
	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRender(t, script, err)

	// Init config block wins for load
	assertContains(t, s, "init { base config }")
	assertNotContains(t, s, "pushed { config }")
	// Init commands before pushed commands, single commit
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")
	assertNotContains(t, s, "merge")

	initIdx := strings.Index(s, "set init-cmd")
	pushIdx := strings.Index(s, "set pushed-cmd")
	if initIdx > pushIdx {
		t.Fatal("init commands must come before pushed commands")
	}
}

func TestRenderMergedScript_InitCommandsOnly(t *testing.T) {
	ic := &InitConfig{Commands: "set init-cmd"}
	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRender(t, script, err)

	// No init config block → pushed config loaded
	assertContains(t, s, "pushed { config }")
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")

	initIdx := strings.Index(s, "set init-cmd")
	pushIdx := strings.Index(s, "set pushed-cmd")
	if initIdx > pushIdx {
		t.Fatal("init commands must come before pushed commands")
	}
}

func TestRenderMergedScript_NoPushedConfig(t *testing.T) {
	ic := &InitConfig{Config: "init { config }", Commands: "set init-cmd"}
	script, err := ic.RenderMergedScript("", "set pushed-cmd")
	s := mustRender(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")
}

func TestRenderMergedScript_NoPushedAnything(t *testing.T) {
	ic := &InitConfig{Config: "init { config }", Commands: "set init-cmd"}
	script, err := ic.RenderMergedScript("", "")
	s := mustRender(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertNotContains(t, s, "pushed")
}

// --- RenderSimpleScript (no init config) ---

func TestRenderSimpleScript(t *testing.T) {
	script, err := RenderSimpleScript("my { config }", "set my-cmd")
	s := mustRender(t, script, err)

	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "my { config }")
	assertContains(t, s, "set my-cmd")
	assertNotContains(t, s, "init config")
}

func TestRenderSimpleScriptNoConfig(t *testing.T) {
	script, err := RenderSimpleScript("", "set cmd")
	s := mustRender(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	assertContains(t, s, "set cmd")
}

// --- helpers ---

func mustRender(t *testing.T, script []byte, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	return string(script)
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected to contain %q, got:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected NOT to contain %q, got:\n%s", substr, s)
	}
}

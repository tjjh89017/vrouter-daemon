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
  set system name-server 8.8.8.8
after_config: |
  interfaces {
    ethernet eth0 {
      address dhcp
    }
  }
after_commands: |
  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
  set firewall name MGMT rule 10 action accept
`
	os.WriteFile(path, []byte(content), 0644)

	ic, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig error: %v", err)
	}
	assertContains(t, ic.Config, "ethernet eth0")
	assertContains(t, ic.Commands, "name-server")
	assertContains(t, ic.AfterConfig, "ethernet eth0")
	assertContains(t, ic.AfterCommands, "set protocols static route")
	if ic.IsEmpty() {
		t.Fatal("expected non-empty")
	}
	if !ic.HasAfter() {
		t.Fatal("expected HasAfter=true")
	}
}

func TestLoadInitConfigEmpty(t *testing.T) {
	ic, err := LoadInitConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ic.IsEmpty() {
		t.Fatal("expected empty")
	}
	if ic.HasAfter() {
		t.Fatal("expected HasAfter=false")
	}
}

func TestLoadInitConfigBeforeOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "init.yaml")
	os.WriteFile(path, []byte("commands: |\n  set foo\n"), 0644)

	ic, err := LoadInitConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if ic.IsEmpty() {
		t.Fatal("expected non-empty")
	}
	if ic.HasAfter() {
		t.Fatal("expected HasAfter=false")
	}
}

// --- RenderScript (failover) ---

func TestRenderScript_BeforeAndAfter(t *testing.T) {
	ic := &InitConfig{
		Config:        "init { config }",
		Commands:      "set init-cmd",
		AfterConfig:   "after { config }",
		AfterCommands: "set after-cmd",
	}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)

	assertContains(t, s, "load /dev/stdin")
	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "merge /tmp/vrouter-after.config")
	assertContains(t, s, "set after-cmd")
	// Two commits
	if strings.Count(s, "commit") != 2 {
		t.Fatalf("expected 2 commits, got %d in:\n%s", strings.Count(s, "commit"), s)
	}
}

func TestRenderScript_BeforeOnly(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { config }",
		Commands: "set init-cmd",
	}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertNotContains(t, s, "merge")
	// Single commit
	if strings.Count(s, "commit") != 1 {
		t.Fatalf("expected 1 commit, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderScript_AfterOnly(t *testing.T) {
	ic := &InitConfig{
		AfterCommands: "set after-cmd",
	}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)

	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	assertContains(t, s, "set after-cmd")
	if strings.Count(s, "commit") != 2 {
		t.Fatalf("expected 2 commits, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderScript_Empty(t *testing.T) {
	ic := &InitConfig{}
	script, err := ic.RenderScript()
	s := mustRender(t, script, err)

	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
	if strings.Count(s, "commit") != 1 {
		t.Fatalf("expected 1 commit, got %d", strings.Count(s, "commit"))
	}
}

// --- RenderMergedScript (server push + init config) ---

func TestRenderMergedScript_FullFlow(t *testing.T) {
	ic := &InitConfig{
		Config:        "init { base }",
		Commands:      "set init-cmd",
		AfterConfig:   "after { protect }",
		AfterCommands: "set after-cmd",
	}
	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRender(t, script, err)

	// Phase 1: load init config (wins over pushed), init commands, pushed commands, commit
	assertContains(t, s, "init { base }")
	assertNotContains(t, s, "pushed { config }") // before config wins
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")

	// Phase 2: merge after config, after commands, commit
	assertContains(t, s, "merge /tmp/vrouter-after.config")
	assertContains(t, s, "set after-cmd")

	// Ordering: init-cmd → pushed-cmd → merge → after-cmd
	initIdx := strings.Index(s, "set init-cmd")
	pushedIdx := strings.Index(s, "set pushed-cmd")
	mergeIdx := strings.Index(s, "merge")
	afterIdx := strings.Index(s, "set after-cmd")
	if initIdx > pushedIdx || pushedIdx > mergeIdx || mergeIdx > afterIdx {
		t.Fatal("wrong ordering")
	}

	if strings.Count(s, "commit") != 2 {
		t.Fatalf("expected 2 commits, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderMergedScript_BeforeOnlyInit(t *testing.T) {
	ic := &InitConfig{
		Config:   "init { config }",
		Commands: "set init-cmd",
	}
	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRender(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set init-cmd")
	assertContains(t, s, "set pushed-cmd")
	assertNotContains(t, s, "merge")
	if strings.Count(s, "commit") != 1 {
		t.Fatalf("expected 1 commit, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderMergedScript_AfterOnlyInit(t *testing.T) {
	ic := &InitConfig{
		AfterCommands: "set after-cmd",
	}
	script, err := ic.RenderMergedScript("pushed { config }", "set pushed-cmd")
	s := mustRender(t, script, err)

	// No before config → pushed config loaded
	assertContains(t, s, "pushed { config }")
	assertContains(t, s, "set pushed-cmd")
	assertContains(t, s, "set after-cmd")
	if strings.Count(s, "commit") != 2 {
		t.Fatalf("expected 2 commits, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderMergedScript_NoPushed(t *testing.T) {
	ic := &InitConfig{
		Config:        "init { config }",
		AfterCommands: "set after-cmd",
	}
	script, err := ic.RenderMergedScript("", "")
	s := mustRender(t, script, err)

	assertContains(t, s, "init { config }")
	assertContains(t, s, "set after-cmd")
	assertNotContains(t, s, "pushed")
}

// --- RenderSimpleScript (no init config) ---

func TestRenderSimpleScript(t *testing.T) {
	script, err := RenderSimpleScript("my { config }", "set my-cmd")
	s := mustRender(t, script, err)

	assertContains(t, s, "my { config }")
	assertContains(t, s, "set my-cmd")
	assertNotContains(t, s, "merge")
	if strings.Count(s, "commit") != 1 {
		t.Fatalf("expected 1 commit, got %d", strings.Count(s, "commit"))
	}
}

func TestRenderSimpleScriptNoConfig(t *testing.T) {
	script, err := RenderSimpleScript("", "set cmd")
	s := mustRender(t, script, err)
	assertContains(t, s, "load /opt/vyatta/etc/config.boot.default")
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

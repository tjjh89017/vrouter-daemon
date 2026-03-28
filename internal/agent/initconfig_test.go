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

func TestRenderScript(t *testing.T) {
	ic := &InitConfig{
		Config: `interfaces {
  ethernet eth0 {
    address dhcp
  }
}`,
		Commands: "set protocols static route 0.0.0.0/0 next-hop 192.168.1.1",
	}

	script, err := ic.RenderScript()
	if err != nil {
		t.Fatalf("RenderScript error: %v", err)
	}

	s := string(script)
	if !strings.Contains(s, "#!/bin/vbash") {
		t.Fatal("expected shebang")
	}
	if !strings.Contains(s, "load /dev/stdin") {
		t.Fatal("expected load /dev/stdin")
	}
	if !strings.Contains(s, "ethernet eth0") {
		t.Fatal("expected config content")
	}
	if !strings.Contains(s, "set protocols static route") {
		t.Fatal("expected commands content")
	}
	if !strings.Contains(s, "commit") {
		t.Fatal("expected commit")
	}
	if !strings.Contains(s, "save") {
		t.Fatal("expected save")
	}
}

func TestRenderScriptNoConfig(t *testing.T) {
	ic := &InitConfig{
		Commands: "set interfaces ethernet eth0 address dhcp",
	}

	script, err := ic.RenderScript()
	if err != nil {
		t.Fatalf("RenderScript error: %v", err)
	}

	s := string(script)
	if !strings.Contains(s, "load /opt/vyatta/etc/config.boot.default") {
		t.Fatal("expected default config load when no config provided")
	}
	if !strings.Contains(s, "set interfaces ethernet eth0") {
		t.Fatal("expected commands content")
	}
}

func TestRenderScriptEmpty(t *testing.T) {
	ic := &InitConfig{}

	script, err := ic.RenderScript()
	if err != nil {
		t.Fatalf("RenderScript error: %v", err)
	}

	s := string(script)
	if !strings.Contains(s, "load /opt/vyatta/etc/config.boot.default") {
		t.Fatal("expected default config load")
	}
	if !strings.Contains(s, "commit") {
		t.Fatal("expected commit")
	}
}

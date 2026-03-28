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
}

func TestMerge(t *testing.T) {
	ic := &InitConfig{
		Commands: "set firewall name MGMT rule 10 action accept",
	}

	config, commands := ic.Merge("", "set interfaces ethernet eth0 address 10.0.0.1/24")

	// No config from either side → empty (template loads default)
	if config != "" {
		t.Fatalf("expected empty config, got: %s", config)
	}

	// Init commands first, then pushed commands
	if !strings.Contains(commands, "set interfaces ethernet eth0") {
		t.Fatal("expected pushed commands in merged output")
	}
	if !strings.Contains(commands, "set firewall name MGMT") {
		t.Fatal("expected init commands in merged output")
	}
	// Init commands must come BEFORE pushed commands
	initIdx := strings.Index(commands, "set firewall")
	pushIdx := strings.Index(commands, "set interfaces")
	if initIdx > pushIdx {
		t.Fatal("expected init commands before pushed commands")
	}
}

func TestMergeWithPushedConfig(t *testing.T) {
	ic := &InitConfig{
		Config:   "init config block",
		Commands: "set init cmd",
	}

	// Init config block takes priority
	config, commands := ic.Merge("pushed config block", "set pushed cmd")

	if config != "init config block" {
		t.Fatalf("expected init config block, got: %s", config)
	}
	if !strings.Contains(commands, "set pushed cmd") {
		t.Fatal("expected pushed commands")
	}
	if !strings.Contains(commands, "set init cmd") {
		t.Fatal("expected init commands")
	}
	// Init commands before pushed
	initIdx := strings.Index(commands, "set init cmd")
	pushIdx := strings.Index(commands, "set pushed cmd")
	if initIdx > pushIdx {
		t.Fatal("expected init commands before pushed commands")
	}
}

func TestMergeInitConfigBlockWins(t *testing.T) {
	ic := &InitConfig{
		Config:   "init config block",
		Commands: "set init cmd",
	}

	config, _ := ic.Merge("pushed config block", "set pushed cmd")

	if config != "init config block" {
		t.Fatalf("expected init config block to win, got: %s", config)
	}
}

func TestMergeFallsToPushedConfigWhenNoInitConfig(t *testing.T) {
	ic := &InitConfig{
		Commands: "set init cmd",
	}

	config, _ := ic.Merge("pushed config block", "")

	if config != "pushed config block" {
		t.Fatalf("expected pushed config block when init has no config, got: %s", config)
	}
}

func TestRenderMergedScript(t *testing.T) {
	ic := &InitConfig{
		Commands: "set firewall name MGMT rule 10 action accept",
	}

	script, err := ic.RenderMergedScript(
		`interfaces { ethernet eth0 { address dhcp } }`,
		"set protocols static route 0.0.0.0/0 next-hop 192.168.1.1",
	)
	if err != nil {
		t.Fatalf("RenderMergedScript error: %v", err)
	}

	s := string(script)
	if !strings.Contains(s, "load /dev/stdin") {
		t.Fatal("expected load /dev/stdin for pushed config")
	}
	if !strings.Contains(s, "ethernet eth0") {
		t.Fatal("expected pushed config in script")
	}
	if !strings.Contains(s, "set protocols static route") {
		t.Fatal("expected pushed commands in script")
	}
	if !strings.Contains(s, "set firewall name MGMT") {
		t.Fatal("expected init commands in script")
	}
	if !strings.Contains(s, "init config commands (protected)") {
		t.Fatal("expected init config marker comment")
	}
}

func TestRenderMergedScriptNoPushedConfig(t *testing.T) {
	ic := &InitConfig{
		Commands: "set firewall name MGMT rule 10 action accept",
	}

	script, err := ic.RenderMergedScript("", "set pushed cmd")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	s := string(script)
	// No pushed config and no init config block → load default
	if !strings.Contains(s, "load /opt/vyatta/etc/config.boot.default") {
		t.Fatal("expected default config load")
	}
}

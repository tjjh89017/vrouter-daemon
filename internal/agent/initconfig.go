package agent

import (
	"bytes"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// InitConfig holds the fallback configuration that guarantees management
// connectivity. When the agent cannot reach the server after maxRetries,
// it applies this config to restore a known-good state.
//
// The structure mirrors VRouterConfigSpec from vrouter-operator:
//   - Config:   VyOS config block loaded via "load /dev/stdin"
//   - Commands: VyOS configure-mode commands executed in configure mode after load
//
// YAML format:
//
//	config: |
//	  interfaces {
//	    ethernet eth0 {
//	      address dhcp
//	    }
//	  }
//	commands: |
//	  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
//	  set firewall name MGMT rule 10 action accept
type InitConfig struct {
	Config   string `yaml:"config"`   // VyOS config block (optional)
	Commands string `yaml:"commands"` // VyOS configure-mode commands (optional)
}

// scriptData is the data passed to the vbash script template.
type scriptData struct {
	Config         string // config block for "load" (init > pushed > default)
	InitCommands   string // init commands (run first)
	PushedCommands string // pushed commands (run after init)
}

// Single load, all commands in one shot, single commit.
//
// Order:
//  1. load config block (init config > pushed config > VyOS default)
//  2. init commands (protected baseline)
//  3. pushed commands (overlay)
//  4. commit + save
var applyScriptTmpl = template.Must(template.New("apply").Parse(`#!/bin/vbash
if [ "$(id -g -n)" != 'vyattacfg' ] ; then
    exec sg vyattacfg -c "/bin/vbash $(readlink -f $0) $@"
fi
source /opt/vyatta/etc/functions/script-template
configure

# --- config section ---
{{- if .Config }}
load /dev/stdin <<'VYOS_CONFIG_EOF'
{{ .Config }}
VYOS_CONFIG_EOF
{{- else }}
load /opt/vyatta/etc/config.boot.default
{{- end }}
{{- if .InitCommands }}

# --- init config commands (protected) ---
{{ .InitCommands }}
{{- end }}
{{- if .PushedCommands }}

# --- pushed commands ---
{{ .PushedCommands }}
{{- end }}

commit
save
`))

// LoadInitConfig reads an init config YAML file.
func LoadInitConfig(path string) (*InitConfig, error) {
	if path == "" {
		return &InitConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ic := &InitConfig{}
	if err := yaml.Unmarshal(data, ic); err != nil {
		return nil, err
	}

	return ic, nil
}

// IsEmpty returns true if both Config and Commands are empty.
func (ic *InitConfig) IsEmpty() bool {
	return ic.Config == "" && ic.Commands == ""
}

// RenderScript generates a vbash script from the init config alone (failover).
func (ic *InitConfig) RenderScript() ([]byte, error) {
	return renderScript(scriptData{
		Config:       ic.Config,
		InitCommands: strings.TrimRight(ic.Commands, "\n"),
	})
}

// RenderMergedScript generates a vbash script with init config as base layer.
// Config block priority: init config > pushed config > VyOS default.
// Commands order: init commands first, then pushed commands. Single commit.
func (ic *InitConfig) RenderMergedScript(pushedConfig, pushedCommands string) ([]byte, error) {
	// Config block: init config wins
	config := ic.Config
	if config == "" {
		config = pushedConfig
	}

	return renderScript(scriptData{
		Config:         config,
		InitCommands:   strings.TrimRight(ic.Commands, "\n"),
		PushedCommands: strings.TrimRight(pushedCommands, "\n"),
	})
}

// RenderSimpleScript generates a vbash script without init config.
func RenderSimpleScript(config, commands string) ([]byte, error) {
	return renderScript(scriptData{
		Config:         config,
		PushedCommands: strings.TrimRight(commands, "\n"),
	})
}

func renderScript(data scriptData) ([]byte, error) {
	var buf bytes.Buffer
	if err := applyScriptTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

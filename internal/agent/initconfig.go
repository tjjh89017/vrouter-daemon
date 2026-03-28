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

// PushedConfigFile is where the agent writes pushed config before executing
// a merged script that uses "merge".
const PushedConfigFile = "/tmp/vrouter-pushed.config"

// scriptData is the data passed to the vbash script template.
type scriptData struct {
	InitConfig     string // init config block (loaded via "load")
	InitCommands   string // init commands (run first)
	PushedConfig   string // pushed config block (merged via "merge" from file)
	PushedCommands string // pushed commands (run after init commands)
}

// Script order when init config is present:
//  1. load init config (or default if no init config block)
//  2. init commands (protected base)
//  3. merge pushed config from file (overlay)
//  4. pushed commands (overlay)
//  5. commit + save
//
// Script order when NO init config:
//  1. load pushed config (or default)
//  2. pushed commands
//  3. commit + save
var applyScriptTmpl = template.Must(template.New("apply").Parse(`#!/bin/vbash
if [ "$(id -g -n)" != 'vyattacfg' ] ; then
    exec sg vyattacfg -c "/bin/vbash $(readlink -f $0) $@"
fi
source /opt/vyatta/etc/functions/script-template
configure

# --- load base config ---
{{- if .InitConfig }}
load /dev/stdin <<'VYOS_CONFIG_EOF'
{{ .InitConfig }}
VYOS_CONFIG_EOF
{{- else if .PushedConfig }}
load /dev/stdin <<'VYOS_CONFIG_EOF'
{{ .PushedConfig }}
VYOS_CONFIG_EOF
{{- else }}
load /opt/vyatta/etc/config.boot.default
{{- end }}
{{- if .InitCommands }}

# --- init config commands (protected) ---
{{ .InitCommands }}
{{- end }}
{{- if and .InitConfig .PushedConfig }}

# --- merge pushed config ---
merge /tmp/vrouter-pushed.config
{{- end }}
{{- if .PushedCommands }}

# --- pushed commands ---
{{ .PushedCommands }}
{{- end }}

commit
save
`))

// LoadInitConfig reads an init config YAML file.
// The file should contain "config" and/or "commands" fields.
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
		InitConfig:   ic.Config,
		InitCommands: strings.TrimRight(ic.Commands, "\n"),
	})
}

// RenderMergedScript generates a vbash script with init config as base and
// pushed config/commands as overlay.
//
// If both init config block AND pushed config block are present, the pushed
// config is merged via "merge /tmp/vrouter-pushed.config". The caller must
// write the pushed config to PushedConfigFile before executing the script.
func (ic *InitConfig) RenderMergedScript(pushedConfig, pushedCommands string) ([]byte, error) {
	return renderScript(scriptData{
		InitConfig:     ic.Config,
		InitCommands:   strings.TrimRight(ic.Commands, "\n"),
		PushedConfig:   pushedConfig,
		PushedCommands: strings.TrimRight(pushedCommands, "\n"),
	})
}

// RenderSimpleScript generates a vbash script without init config.
func RenderSimpleScript(config, commands string) ([]byte, error) {
	return renderScript(scriptData{
		PushedConfig:   config,
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

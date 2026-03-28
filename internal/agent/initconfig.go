package agent

import (
	"bytes"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// InitConfig holds the init configuration for the agent.
// "Before" fields establish a baseline before pushed config.
// "After" fields provide a protection layer applied after pushed config is committed.
//
// YAML format:
//
//	config: |
//	  interfaces { ethernet eth0 { address dhcp } }
//	commands: |
//	  set system name-server 8.8.8.8
//	after_config: |
//	  interfaces { ethernet eth0 { address dhcp } }
//	after_commands: |
//	  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
//	  set firewall name MGMT rule 10 action accept
type InitConfig struct {
	Config        string `yaml:"config"`         // before: VyOS config block (loaded via "load")
	Commands      string `yaml:"commands"`        // before: VyOS configure-mode commands
	AfterConfig   string `yaml:"after_config"`    // after: VyOS config block (merged via "merge" after commit)
	AfterCommands string `yaml:"after_commands"`  // after: VyOS configure-mode commands (after commit)
}

// AfterConfigFile is the temp file path for the after-phase config block merge.
const AfterConfigFile = "/tmp/vrouter-after.config"

// scriptData is the data passed to the vbash script template.
type scriptData struct {
	Config         string // before config block (for load)
	InitCommands   string // before commands
	PushedCommands string // pushed commands from server
	AfterConfig    string // after config block (for merge, requires prior commit)
	AfterCommands  string // after commands
}

// Script flow:
//  1. load [before config > pushed config > default]
//  2. before commands
//  3. pushed commands
//  4. commit
//  5. (only if after_config or after_commands)
//     merge after_config + after_commands + commit
//  6. save
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

# --- init commands ---
{{ .InitCommands }}
{{- end }}
{{- if .PushedCommands }}

# --- pushed commands ---
{{ .PushedCommands }}
{{- end }}

commit
{{- if or .AfterConfig .AfterCommands }}

# --- after config (protection layer) ---
{{- if .AfterConfig }}
merge /tmp/vrouter-after.config
{{- end }}
{{- if .AfterCommands }}
{{ .AfterCommands }}
{{- end }}

commit
{{- end }}

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

// IsEmpty returns true if all fields are empty.
func (ic *InitConfig) IsEmpty() bool {
	return ic.Config == "" && ic.Commands == "" && ic.AfterConfig == "" && ic.AfterCommands == ""
}

// HasAfter returns true if any after-phase fields are set.
func (ic *InitConfig) HasAfter() bool {
	return ic.AfterConfig != "" || ic.AfterCommands != ""
}

// RenderScript generates a vbash script from the init config alone (failover).
// Uses both before and after phases.
func (ic *InitConfig) RenderScript() ([]byte, error) {
	return renderScript(scriptData{
		Config:        ic.Config,
		InitCommands:  trimNL(ic.Commands),
		AfterConfig:   ic.AfterConfig,
		AfterCommands: trimNL(ic.AfterCommands),
	})
}

// RenderMergedScript generates a vbash script with init config + pushed config.
// Before config block takes priority over pushed config block for load.
// After config/commands apply after the first commit as a protection layer.
func (ic *InitConfig) RenderMergedScript(pushedConfig, pushedCommands string) ([]byte, error) {
	config := ic.Config
	if config == "" {
		config = pushedConfig
	}

	return renderScript(scriptData{
		Config:         config,
		InitCommands:   trimNL(ic.Commands),
		PushedCommands: trimNL(pushedCommands),
		AfterConfig:    ic.AfterConfig,
		AfterCommands:  trimNL(ic.AfterCommands),
	})
}

// RenderSimpleScript generates a vbash script without init config.
func RenderSimpleScript(config, commands string) ([]byte, error) {
	return renderScript(scriptData{
		Config:         config,
		PushedCommands: trimNL(commands),
	})
}

func renderScript(data scriptData) ([]byte, error) {
	var buf bytes.Buffer
	if err := applyScriptTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func trimNL(s string) string {
	return strings.TrimRight(s, "\n")
}

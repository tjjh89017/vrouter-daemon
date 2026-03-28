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
	Config   string
	Commands string
}

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

# --- commands section ---
{{- if .Commands }}
{{ .Commands }}
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
// If Config is empty, loads the VyOS default config.
func (ic *InitConfig) RenderScript() ([]byte, error) {
	return renderScript(ic.Config, ic.Commands)
}

// Merge combines pushed config/commands with init config.
// Init config commands run FIRST (base layer), then pushed commands on top.
// This ensures init config establishes the baseline, and pushed config can
// override non-protected settings.
// For the config block: init config > pushed config > VyOS default.
func (ic *InitConfig) Merge(pushedConfig, pushedCommands string) (mergedConfig, mergedCommands string) {
	// Config block: init config wins, then pushed, then default
	switch {
	case ic.Config != "":
		mergedConfig = ic.Config
	case pushedConfig != "":
		mergedConfig = pushedConfig
	default:
		mergedConfig = "" // template will load default
	}

	// Commands: init config first (base), then pushed commands (overlay)
	var parts []string
	if ic.Commands != "" {
		parts = append(parts, "# --- init config commands (protected) ---")
		parts = append(parts, strings.TrimRight(ic.Commands, "\n"))
	}
	if pushedCommands != "" {
		parts = append(parts, strings.TrimRight(pushedCommands, "\n"))
	}
	mergedCommands = strings.Join(parts, "\n")

	return mergedConfig, mergedCommands
}

// RenderMergedScript generates a vbash script with pushed config/commands
// merged with init config.
func (ic *InitConfig) RenderMergedScript(pushedConfig, pushedCommands string) ([]byte, error) {
	config, commands := ic.Merge(pushedConfig, pushedCommands)
	return renderScript(config, commands)
}

func renderScript(config, commands string) ([]byte, error) {
	var buf bytes.Buffer
	if err := applyScriptTmpl.Execute(&buf, scriptData{
		Config:   config,
		Commands: commands,
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

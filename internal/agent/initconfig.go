package agent

import (
	"bytes"
	"os"
	"text/template"

	"gopkg.in/yaml.v3"
)

// InitConfig holds the fallback configuration that guarantees management
// connectivity. When the agent cannot reach the server after maxRetries,
// it applies this config to restore a known-good state.
//
// The structure mirrors VRouterConfigSpec from vrouter-operator:
//   - Config:   VyOS config block loaded via "load /dev/stdin"
//   - Commands: VyOS commands executed in configure mode after load
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

var initScriptTmpl = template.Must(template.New("init").Parse(`#!/bin/vbash
if [ "$(id -g -n)" != 'vyattacfg' ] ; then
    exec sg vyattacfg -c "/bin/vbash $(readlink -f $0) $@"
fi
source /opt/vyatta/etc/functions/script-template
configure

# --- init config section ---
{{- if .Config }}
load /dev/stdin <<'VYOS_CONFIG_EOF'
{{ .Config }}
VYOS_CONFIG_EOF
{{- else }}
load /opt/vyatta/etc/config.boot.default
{{- end }}

# --- init commands section ---
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

// RenderScript generates a vbash script that applies the init config.
// The script format matches the vrouter-operator's apply script:
// configure → load config → run commands → commit → save.
func (ic *InitConfig) RenderScript() ([]byte, error) {
	var buf bytes.Buffer
	if err := initScriptTmpl.Execute(&buf, ic); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

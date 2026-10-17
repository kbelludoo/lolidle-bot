// Package opencode reads OpenCode's own config (~/.config/opencode/opencode.json)
// and credentials (~/.local/share/opencode/auth.json) so this agent reuses the
// logins already set up with `opencode auth login`.
package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent/provider"
)

type providerConfig struct {
	NPM     string `json:"npm"`
	Name    string `json:"name"`
	Options struct {
		BaseURL string            `json:"baseURL"`
		APIKey  string            `json:"apiKey"`
		Headers map[string]string `json:"headers"`
	} `json:"options"`
	Models map[string]struct {
		Name    string            `json:"name"`
		Headers map[string]string `json:"headers"`
	} `json:"models"`
}

type Config struct {
	Model    string                    `json:"model"`
	Provider map[string]providerConfig `json:"provider"`
}

type authEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type Resolved struct {
	Provider   provider.Provider
	ProviderID string
	Model      string
	Name       string
}

func configDir() string {
	if v := os.Getenv("OPENCODE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "opencode")
}

func dataDir() string {
	if v := os.Getenv("OPENCODE_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "opencode")
}

func LoadConfig(dir string) (*Config, error) {
	var path string
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("no opencode.json/.jsonc in %s", dir)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONC(raw), &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

func loadAuth(dir string) map[string]authEntry {
	raw, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		return nil
	}
	var m map[string]authEntry
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// Resolve turns a "provider/model" reference (or the config default when ref is
// empty) into a ready-to-use provider, merging apiKey/headers from the opencode
// config and falling back to auth.json for the key.
func Resolve(ref string) (*Resolved, error) {
	cfg, err := LoadConfig(configDir())
	if err != nil {
		return nil, err
	}
	if ref == "" {
		ref = cfg.Model
	}
	if ref == "" {
		return nil, fmt.Errorf("no model set; pass provider/model")
	}

	providerID, modelID, ok := strings.Cut(ref, "/")
	if !ok || providerID == "" || modelID == "" {
		return nil, fmt.Errorf("model ref %q must be provider/model", ref)
	}

	pc, ok := cfg.Provider[providerID]
	if !ok {
		return nil, fmt.Errorf("provider %q not in opencode config", providerID)
	}

	key := pc.Options.APIKey
	if key == "" {
		if a, ok := loadAuth(dataDir())[providerID]; ok {
			key = a.Key
		}
	}

	headers := map[string]string{}
	for k, v := range pc.Options.Headers {
		headers[k] = v
	}
	if m, ok := pc.Models[modelID]; ok {
		for k, v := range m.Headers {
			headers[k] = v
		}
	}

	name := pc.Name
	if name == "" {
		name = providerID
	}

	var p provider.Provider
	if strings.Contains(pc.NPM, "anthropic") {
		p = &provider.Anthropic{BaseURL: pc.Options.BaseURL, APIKey: key, Model: modelID, Headers: headers}
	} else {
		p = &provider.OpenAI{BaseURL: pc.Options.BaseURL, APIKey: key, Model: modelID, Headers: headers}
	}

	return &Resolved{Provider: p, ProviderID: providerID, Model: modelID, Name: name}, nil
}

// stripJSONC removes // and /* */ comments so jsonc files parse as json.
func stripJSONC(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(b) {
			if b[i+1] == '/' {
				for i < len(b) && b[i] != '\n' {
					i++
				}
				out = append(out, '\n')
				continue
			}
			if b[i+1] == '*' {
				i += 2
				for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

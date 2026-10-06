package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config vit dans ~/.config/bifrost/config.json (ou l'équivalent de l'OS),
// en 0600 : elle porte le jeton API. Jamais de mot de passe SSH ici.
type Config struct {
	SiteURL  string `json:"site_url"`
	APIToken string `json:"api_token"`
	// Dossier où garder les .torrent publiés quand aucun client n'est configuré.
	OutDir string `json:"out_dir"`
	// Dernier dossier parcouru, pour rouvrir la page au même endroit.
	LastDir string `json:"last_dir,omitempty"`
	// Source des fichiers : "local" (ce poste) ou "ssh" (seedbox, docs/23 §4).
	Source string    `json:"source"`
	SSH    SSHConfig `json:"ssh"`
	// Client torrent qui seedera après publication (docs/23 §9.1).
	Client ClientConfig `json:"client"`
	// Mot de passe local (argon2id), exigé hors loopback (docs/23 §9.2).
	UIPasswordHash string `json:"ui_password_hash,omitempty"`
	// Mise à jour automatique au lancement (docs/23 §9.3) ; nil = oui.
	AutoUpdate *bool `json:"auto_update,omitempty"`
}

func (c *Config) autoUpdate() bool { return c.AutoUpdate == nil || *c.AutoUpdate }

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "bifrost", "config.json")
}

func defaultOutDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "Bifrost"
	}
	return filepath.Join(home, "Bifrost")
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{OutDir: defaultOutDir(), Source: "local"}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.OutDir == "" {
		cfg.OutDir = defaultOutDir()
	}
	if cfg.Source == "" {
		cfg.Source = "local"
	}
	return cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

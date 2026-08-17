package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	General   GeneralConfig   `toml:"general"`
	TMDB      TMDBConfig      `toml:"tmdb"`
	MPV       MPVConfig       `toml:"mpv"`
	Providers ProvidersConfig `toml:"providers"`
}

type GeneralConfig struct {
	DataDir string `toml:"data_dir"`
}

type TMDBConfig struct {
	APIKey string `toml:"api_key"`
}

type MPVConfig struct {
	Path string `toml:"path"`
}

type ProvidersConfig struct {
	Movie []string `toml:"movie"`
	TV    []string `toml:"tv"`
	Anime []string `toml:"anime"`
}

// DataDir is fixed per spec: everything lives on G:, off the NVMe, in one place.
const DataDir = `G:\ReelShell`

func ConfigPath() string {
	return filepath.Join(DataDir, "config.toml")
}

// Load reads config.toml from DataDir, creating the directory layout and a
// default config on first run. Fails fast (rather than falling back to some
// other drive) if G: isn't reachable, per spec.
func Load() (*Config, error) {
	path := ConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig()
		if err := Save(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	} else if err != nil {
		return nil, err
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	for _, sub := range []string{"", "providers", filepath.Join("cache", "torrents"), "logs"} {
		if err := os.MkdirAll(filepath.Join(DataDir, sub), 0o755); err != nil {
			return err
		}
	}

	f, err := os.Create(ConfigPath())
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}

func defaultConfig() *Config {
	return &Config{
		General: GeneralConfig{DataDir: DataDir},
		TMDB:    TMDBConfig{APIKey: ""},
		MPV:     MPVConfig{Path: ""},
		Providers: ProvidersConfig{
			Movie: []string{filepath.Join(DataDir, "providers", "movie-provider.exe")},
			TV:    []string{filepath.Join(DataDir, "providers", "tv-provider.exe")},
			Anime: []string{filepath.Join(DataDir, "providers", "anime-nyaa.exe")},
		},
	}
}

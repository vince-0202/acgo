package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// LoadSettings loads global and project-level settings, merging them.
func LoadSettings() (*Settings, error) {
	v := viper.New()

	home, _ := os.UserHomeDir()
	globalDir := filepath.Join(home, ".acgo")

	v.SetConfigType("yaml")
	v.SetDefault("default_provider", "openai")
	v.SetDefault("api_keys", map[string]string{})
	v.SetDefault("session.root", filepath.Join(globalDir, "sessions"))

	v.SetConfigName("settings")
	v.AddConfigPath(globalDir)
	_ = v.ReadInConfig() // ignore not found

	// Project-level override: .acgo/settings.yaml
	projectDir, _ := os.Getwd()
	projectConfigDir := filepath.Join(projectDir, ".acgo")
	v.AddConfigPath(projectConfigDir)
	_ = v.MergeInConfig()

	var s Settings
	if err := v.Unmarshal(&s); err != nil {
		return nil, err
	}
	if s.Session.Root == "" {
		s.Session.Root = filepath.Join(globalDir, "sessions")
	} else if s.Session.Root[0] == '~' {
		s.Session.Root = expandHome(s.Session.Root)
	}

	// Allow environment variables like OPENAI_API_KEY to override API keys.
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		s.APIKeys["openai"] = key
	}

	s.Log.loadAndInit()

	return &s, nil
}

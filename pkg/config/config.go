package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Settings represents the merged configuration for pi-go.
type Settings struct {
	DefaultProvider string            `mapstructure:"default_provider"`
	DefaultModelID  string            `mapstructure:"default_model_id"`
	APIKeys         map[string]string `mapstructure:"api_keys"`
}

// LoadSettings loads global and project-level settings, merging them.
func LoadSettings() (*Settings, error) {
	v := viper.New()

	home, _ := os.UserHomeDir()
	globalDir := filepath.Join(home, ".gopi", "agent")

	v.SetConfigType("yaml")
	v.SetDefault("default_provider", "openai")
	v.SetDefault("api_keys", map[string]string{})

	v.SetConfigName("settings")
	v.AddConfigPath(globalDir)
	_ = v.ReadInConfig() // ignore not found

	// Project-level override: .gopi/settings.yaml
	projectDir, _ := os.Getwd()
	projectConfigDir := filepath.Join(projectDir, ".gopi")
	v.AddConfigPath(projectConfigDir)
	_ = v.MergeInConfig()

	var s Settings
	if err := v.Unmarshal(&s); err != nil {
		return nil, err
	}

	// Allow environment variables like OPENAI_API_KEY to override API keys.
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		s.APIKeys["openai"] = key
	}

	return &s, nil
}


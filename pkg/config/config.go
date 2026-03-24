package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type SettingConfig struct {
	DirectName string
	Name       string
}

type SettingOption func(*SettingConfig)

func WithDirectName(name string) SettingOption {
	return func(config *SettingConfig) {
		config.DirectName = name
	}
}

func WithName(name string) SettingOption {
	return func(config *SettingConfig) {
		config.Name = name
	}
}

func NewSettingConfig(options ...SettingOption) *SettingConfig {
	settingConfig := &SettingConfig{
		DirectName: ".acgo",
		Name:       "settings",
	}
	if len(options) == 0 {
		return settingConfig
	}
	for _, option := range options {
		option(settingConfig)
	}
	return settingConfig
}

func LoadSettingsByConfig(config *SettingConfig) (*Settings, error) {
	v := viper.New()

	home, _ := os.UserHomeDir()
	globalDir := filepath.Join(home, config.DirectName)

	v.SetConfigType("yaml")

	v.SetConfigName(config.Name)
	v.AddConfigPath(globalDir)
	_ = v.ReadInConfig() // ignore not found
	_ = v.MergeInConfig()

	var s Settings
	if err := v.Unmarshal(&s); err != nil {
		return nil, err
	}
	s.WorkDir = globalDir

	s.Log.LoadAndInit()
	s.Agent.LoadAndInit()
	s.Session.LoadAndInit(s.WorkDir)

	return &s, nil
}

// LoadSettings loads global and project-level settings, merging them.
func LoadSettings() (*Settings, error) {
	return LoadSettingsByConfig(NewSettingConfig())
}

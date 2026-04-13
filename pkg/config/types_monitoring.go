package config

import "path/filepath"

type MonitoringConfig struct {
	MetricsFilePath string `mapstructure:"metrics_file_path"`
	LLMFilePath     string `mapstructure:"llm_file_path"`
}

func (m *MonitoringConfig) LoadAndInit(globalDir string) {
	if m == nil {
		return
	}
	if m.MetricsFilePath != "" && m.MetricsFilePath[0] == '~' {
		m.MetricsFilePath = expandHome(m.MetricsFilePath)
	}
	if m.LLMFilePath != "" && m.LLMFilePath[0] == '~' {
		m.LLMFilePath = expandHome(m.LLMFilePath)
	}
	if m.MetricsFilePath != "" && !filepath.IsAbs(m.MetricsFilePath) {
		m.MetricsFilePath = filepath.Join(globalDir, m.MetricsFilePath)
	}
	if m.LLMFilePath != "" && !filepath.IsAbs(m.LLMFilePath) {
		m.LLMFilePath = filepath.Join(globalDir, m.LLMFilePath)
	}
}

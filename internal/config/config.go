package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// AppConfig holds all runtime configuration, loaded from a JSON file.
type AppConfig struct {
	Host           string  `json:"host"`
	Community      string  `json:"community"`
	Port           uint16  `json:"port"`
	SNMPVersion    string  `json:"snmpVersion"`
	InterfaceIndex int     `json:"interfaceIndex"`
	DownloadOID    string  `json:"downloadOID"`
	UploadOID      string  `json:"uploadOID"`
	TimeoutMs      int     `json:"timeoutMs"`
	Retries        int     `json:"retries"`
	WindowWidthDp  float32 `json:"windowWidthDp,omitempty"`
	WindowHeightDp float32 `json:"windowHeightDp,omitempty"`
}

// LoadConfig reads and validates the JSON configuration file at filePath,
// applying sensible defaults for any omitted optional fields.
func LoadConfig(filePath string) (*AppConfig, error) {
	fileBytes, readError := os.ReadFile(filePath)
	if readError != nil {
		return nil, fmt.Errorf("reading config file %q: %w", filePath, readError)
	}

	var appConfig AppConfig
	if unmarshalError := json.Unmarshal(fileBytes, &appConfig); unmarshalError != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", filePath, unmarshalError)
	}

	if appConfig.Host == "" {
		return nil, fmt.Errorf("config field \"host\" is required")
	}
	if appConfig.Community == "" {
		return nil, fmt.Errorf("config field \"community\" is required")
	}

	if appConfig.Port == 0 {
		appConfig.Port = 161
	}
	if appConfig.SNMPVersion == "" {
		appConfig.SNMPVersion = "2c"
	}
	if appConfig.InterfaceIndex == 0 {
		appConfig.InterfaceIndex = 1
	}
	if appConfig.DownloadOID == "" {
		appConfig.DownloadOID = fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.6.%d", appConfig.InterfaceIndex)
	}
	if appConfig.UploadOID == "" {
		appConfig.UploadOID = fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.10.%d", appConfig.InterfaceIndex)
	}
	if appConfig.TimeoutMs == 0 {
		appConfig.TimeoutMs = 3000
	}
	if appConfig.Retries == 0 {
		appConfig.Retries = 1
	}

	return &appConfig, nil
}

// SaveConfig writes cfg back to filePath as indented JSON, preserving all fields.
func SaveConfig(filePath string, cfg *AppConfig) error {
	data, marshalErr := json.MarshalIndent(cfg, "", "\t")
	if marshalErr != nil {
		return fmt.Errorf("marshaling config: %w", marshalErr)
	}
	if writeErr := os.WriteFile(filePath, data, 0644); writeErr != nil {
		return fmt.Errorf("writing config file %q: %w", filePath, writeErr)
	}
	return nil
}

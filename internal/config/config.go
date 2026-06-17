package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// HostConfig holds the SNMP polling configuration for a single monitored
// target. One of these exists per element of AppConfig.Hosts.
type HostConfig struct {
	Host string `json:"host"`
	// Name is an optional human-friendly label shown on the graph instead of the
	// raw host address. When empty, Host is displayed.
	Name        string `json:"name,omitempty"`
	Community   string `json:"community"`
	Port        uint16 `json:"port"`
	SNMPVersion string `json:"snmpVersion"`
	DownloadOID string `json:"downloadOID"`
	UploadOID   string `json:"uploadOID"`
	TimeoutMs   int    `json:"timeoutMs"`
	Retries     int    `json:"retries"`
}

// AppConfig holds general, program-wide configuration plus the list of
// monitored hosts. In the JSON file the top-level object carries the general
// options (window geometry, theme) and each monitored target is an element of
// the "hosts" array.
type AppConfig struct {
	WindowWidthDp  float32 `json:"windowWidthDp,omitempty"`
	WindowHeightDp float32 `json:"windowHeightDp,omitempty"`
	// WindowX and WindowY are the last saved top-left screen position in physical
	// pixels. nil means no saved position (let the OS place the window).
	WindowX *int `json:"windowX,omitempty"`
	WindowY *int `json:"windowY,omitempty"`
	// DarkTheme selects the dark color scheme. nil means dark (the default).
	DarkTheme *bool `json:"darkTheme,omitempty"`

	Hosts []HostConfig `json:"hosts"`
}

// DisplayName returns the label to show for this host: the optional Name if
// set, otherwise the raw Host address.
func (host *HostConfig) DisplayName() string {
	if host.Name != "" {
		return host.Name
	}
	return host.Host
}

// LoadConfig reads and validates the JSON configuration file at filePath,
// applying sensible defaults for any omitted optional fields.
//
// It accepts both the multi-host format (a top-level object with a "hosts"
// array) and the legacy single-host format (a bare host object), which is
// transparently wrapped into a one-element Hosts list.
func LoadConfig(filePath string) (*AppConfig, error) {
	fileBytes, readError := os.ReadFile(filePath)
	if readError != nil {
		return nil, fmt.Errorf("reading config file %q: %w", filePath, readError)
	}

	var appConfig AppConfig
	if unmarshalError := json.Unmarshal(fileBytes, &appConfig); unmarshalError != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", filePath, unmarshalError)
	}

	// Backward compatibility: a legacy config file is a single host object with
	// no "hosts" array. Treat the whole top-level object as one host.
	if len(appConfig.Hosts) == 0 {
		var legacyHost HostConfig
		if unmarshalError := json.Unmarshal(fileBytes, &legacyHost); unmarshalError != nil {
			return nil, fmt.Errorf("parsing config file %q: %w", filePath, unmarshalError)
		}
		appConfig.Hosts = []HostConfig{legacyHost}
	}

	for hostIndex := range appConfig.Hosts {
		if validateError := applyHostDefaults(&appConfig.Hosts[hostIndex]); validateError != nil {
			return nil, fmt.Errorf("hosts[%d]: %w", hostIndex, validateError)
		}
	}

	return &appConfig, nil
}

// applyHostDefaults validates a single host's required fields and fills in
// defaults for any omitted optional fields.
func applyHostDefaults(host *HostConfig) error {
	if host.Host == "" {
		return fmt.Errorf("config field \"host\" is required")
	}
	if host.Community == "" {
		return fmt.Errorf("config field \"community\" is required")
	}

	if host.Port == 0 {
		host.Port = 161
	}
	if host.SNMPVersion == "" {
		host.SNMPVersion = "2c"
	}
	if host.DownloadOID == "" {
		host.DownloadOID = "1.3.6.1.2.1.31.1.1.1.6.1"
	}
	if host.UploadOID == "" {
		host.UploadOID = "1.3.6.1.2.1.31.1.1.1.10.1"
	}
	if host.TimeoutMs == 0 {
		host.TimeoutMs = 3000
	}
	if host.Retries == 0 {
		host.Retries = 1
	}
	return nil
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

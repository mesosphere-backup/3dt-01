package config

import (
	"encoding/json"
	"github.com/pkg/errors"
)

var (
	// Version of 3dt code.
	Version = "0.4.0"

	// APIVer is an API version.
	APIVer = 1
)

// Config structure is a main config object
type Config struct {
	SystemdUnits []string `json:"-"`

	// 3dt flags
	FlagCACertFile                 string `json:"ca-cert"`
	FlagPull                       bool   `json:"pull"`
	FlagVerbose                    bool   `json:"verbose"`
	FlagPort                       int    `json:"port"`
	FlagDisableUnixSocket          bool   `json:"no-unix-socket"`
	FlagMasterPort                 int    `json:"master-port"`
	FlagAgentPort                  int    `json:"agent-port"`
	FlagPullInterval               int    `json:"pull-interval"`
	FlagPullTimeoutSec             int    `json:"pull-timeout"`
	FlagUpdateHealthReportInterval int    `json:"health-update-interval"`
	FlagExhibitorClusterStatusURL  string `json:"exhibitor-ip"`
	FlagForceTLS                   bool   `json:"force-tls"`
	FlagDebug                      bool   `json:"debug"`
	FlagRole                       string `json:"role"`
	FlagIAMConfig                  string `json:"iam-config"`

	// diagnostics job flags
	FlagDiagnosticsBundleDir                     string `json:"diagnostics-bundle-dir"`
	FlagDiagnosticsBundleEndpointsConfigFile     string `json:"endpoint-config"`
	FlagDiagnosticsBundleUnitsLogsSinceString    string `json:"diagnostics-units-since"`
	FlagDiagnosticsJobTimeoutMinutes             int    `json:"diagnostics-job-timeout"`
	FlagDiagnosticsJobGetSingleURLTimeoutMinutes int    `json:"diagnostics-url-timeout"`
	FlagCommandExecTimeoutSec                    int    `json:"command-exec-timeout"`
}

// LoadFromViper takes a map of flags with values and updates the config structure.
func (c *Config) LoadFromViper(settings map[string]interface{}) error {
	// TODO(mnaboka): use a map to struct library
	body, err := json.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "unable to marshal config file")
	}

	if err := json.Unmarshal(body, c); err != nil {
		return errors.Wrap(err, "unable to unmarshal config file")
	}
	return nil
}

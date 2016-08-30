package api

import (
	"flag"
	"os"

	"errors"
	log "github.com/Sirupsen/logrus"
)

var (
	// Version of 3dt code.
	Version = "0.2.11"

	// APIVer is an API version.
	APIVer = 1

	// Revision injected by LDFLAGS a git commit reference.
	Revision string

	// flagSet
	flagSet = flag.NewFlagSet("3dt", flag.ContinueOnError)
)

// Config structure is a main config object
type Config struct {
	Version                                      string
	Revision                                     string
	MesosIPDiscoveryCommand                      string
	DCOSVersion                                  string
	SystemdUnits                                 []string

	// 3dt flags
	FlagCACertFile                               string
	FlagPull                                     bool
	FlagDiag                                     bool
	FlagVerbose                                  bool
	FlagVersion                                  bool
	FlagPort                                     int
	FlagMasterPort                               int
	FlagAgentPort                                int
	FlagPullInterval                             int
	FlagPullTimeoutSec                           int
	FlagUpdateHealthReportInterval               int
	FlagExhibitorClusterStatusURL                string
	FlagForceTLS                                 bool

	// diagnostics job flags
	FlagDiagnosticsBundleDir                     string
	FlagDiagnosticsBundleEndpointsConfigFile     string
	FlagDiagnosticsBundleUnitsLogsSinceString    string
	FlagDiagnosticsJobTimeoutMinutes             int
	FlagDiagnosticsJobGetSingleURLTimeoutMinutes int
	FlagCommandExecTimeoutSec                    int
}

func (c *Config) setFlags(fs *flag.FlagSet) {
	//common flags
	fs.StringVar(&c.FlagCACertFile, "ca-cert", c.FlagCACertFile, "Use certificate authority.")
	fs.BoolVar(&c.FlagDiag, "diag", c.FlagDiag, "Get diagnostics output once on the CLI. Does not expose API.")
	fs.BoolVar(&c.FlagVerbose, "verbose", c.FlagVerbose, "Use verbose debug output.")
	fs.BoolVar(&c.FlagVersion, "version", c.FlagVersion, "Print version.")
	fs.IntVar(&c.FlagPort, "port", c.FlagPort, "Web server TCP port.")
	fs.IntVar(&c.FlagMasterPort, "master-port", c.FlagMasterPort, "Use TCP port to connect to masters.")
	fs.IntVar(&c.FlagAgentPort, "agent-port", c.FlagAgentPort, "Use TCP port to connect to agents.")
	fs.IntVar(&c.FlagCommandExecTimeoutSec, "command-exec-timeout", c.FlagCommandExecTimeoutSec,
		"Set command executing timeout")

	// 3dt flags
	fs.BoolVar(&c.FlagPull, "pull", c.FlagPull, "Try to pull checks from DC/OS hosts.")
	fs.IntVar(&c.FlagPullInterval, "pull-interval", c.FlagPullInterval, "Set pull interval in seconds.")
	fs.IntVar(&c.FlagPullTimeoutSec, "pull-timeout", c.FlagPullTimeoutSec, "Set pull timeout.")
	fs.IntVar(&c.FlagUpdateHealthReportInterval, "health-update-interval", c.FlagUpdateHealthReportInterval,
		"Set update health interval in seconds.")
	fs.StringVar(&c.FlagExhibitorClusterStatusURL, "exhibitor-ip", c.FlagExhibitorClusterStatusURL,
		"Use Exhibitor IP address to discover master nodes.")
	fs.BoolVar(&c.FlagForceTLS, "force-tls", c.FlagForceTLS, "Use HTTPS to do all requests.")

	// diagnostics job flags
	fs.StringVar(&c.FlagDiagnosticsBundleDir, "diagnostics-bundle-dir", c.FlagDiagnosticsBundleDir, "Set a path to store diagnostic bundles")
	fs.StringVar(&c.FlagDiagnosticsBundleEndpointsConfigFile, "endpoint-config", c.FlagDiagnosticsBundleEndpointsConfigFile,
		"Use endpoints_config.json")
	fs.StringVar(&c.FlagDiagnosticsBundleUnitsLogsSinceString, "diagnostics-units-since", c.FlagDiagnosticsBundleUnitsLogsSinceString,
		"Collect systemd units logs since")
	fs.IntVar(&c.FlagDiagnosticsJobTimeoutMinutes, "diagnostics-job-timeout", c.FlagDiagnosticsJobTimeoutMinutes,
		"Set a global diagnostics job timeout")
	fs.IntVar(&c.FlagDiagnosticsJobGetSingleURLTimeoutMinutes, "diagnostics-url-timeout", c.FlagDiagnosticsJobGetSingleURLTimeoutMinutes,
		"Set a local timeout for every single GET request to a log endpoint")
}

// LoadDefaultConfig sets default config values or sets the values from a command line.
func LoadDefaultConfig(args []string) (config Config, err error) {
	if len(args) == 0 {
		return config, errors.New("arguments cannot be empty")
	}

	// default tcp port is 1050
	config.FlagPort = 1050

	// default connect to master port
	config.FlagMasterPort = 1050

	// default connect to agent port
	config.FlagAgentPort = 1050

	// default pulling and health update interval is 60 seconds
	config.FlagPullInterval = 60
	config.FlagUpdateHealthReportInterval = 60

	// Set default pull timeout to 3 seconds
	config.FlagPullTimeoutSec = 3

	config.Version = Version
	config.Revision = Revision

	config.FlagExhibitorClusterStatusURL = "http://127.0.0.1:8181/exhibitor/v1/cluster/status"

	// diagnostics job default flag values
	config.FlagDiagnosticsBundleDir = "/var/run/dcos/3dt/diagnostic_bundles"
	config.FlagDiagnosticsJobTimeoutMinutes = 720 //12 hours

	// 2 minutes for a URL GET timeout.
	config.FlagDiagnosticsJobGetSingleURLTimeoutMinutes = 2

	// 2 minutes for a command to run
	config.FlagCommandExecTimeoutSec = 120

	config.FlagDiagnosticsBundleEndpointsConfigFile = "/opt/mesosphere/etc/endpoints_config.json"
	config.FlagDiagnosticsBundleUnitsLogsSinceString = "24 hours ago"

	detectIPCmd := os.Getenv("MESOS_IP_DISCOVERY_COMMAND")
	if detectIPCmd == "" {
		detectIPCmd = "/opt/mesosphere/bin/detect_ip"
		log.Debugf("Environment variable MESOS_IP_DISCOVERY_COMMAND is not set, using default location: %s", detectIPCmd)
	}
	config.MesosIPDiscoveryCommand = detectIPCmd

	if os.Getenv("DCOS_VERSION") == "" {
		log.Debug("Environment variable DCOS_VERSION is not set")
	}
	config.DCOSVersion = os.Getenv("DCOS_VERSION")
	config.SystemdUnits = []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}

	config.setFlags(flagSet)

	// override with user provided arguments
	if err = flagSet.Parse(args[1:]); err != nil {
		return config, err
	}
	return config, nil
}

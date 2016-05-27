package api

import (
	"flag"
	"os"

	"errors"
	log "github.com/Sirupsen/logrus"
)

var (
	// Version of 3dt code.
	Version = "0.2.0"

	// APIVer is an API version.
	ApiVer int = 1

	// Revision injected by LDFLAGS a git commit reference.
	Revision string

	// flagSet
	flagSet = flag.NewFlagSet("3dt", flag.ContinueOnError)
)

// Config structure is a main config object
type Config struct {
	Version                 string
	Revision                string
	MesosIPDiscoveryCommand string
	DCOSVersion             string
	SystemdUnits            []string

	// 3dt flags
	FlagCACertFile                 string
	FlagPull                       bool
	FlagDiag                       bool
	FlagVerbose                    bool
	FlagVersion                    bool
	FlagPort                       int
	FlagPullInterval               int
	FlagUpdateHealthReportInterval int
	FlagExhibitorClusterStatusURL  string
	FlagForceTLS                   bool

	// snapshot job flags
	FlagSnapshotDir                           string
	FlagSnapshotEndpointsConfigFile           string
	FlagSnapshotUnitsLogsSinceHours           string
	FlagSnapshotJobTimeoutMinutes             int
	FlagSnapshotJobGetSingleURLTimeoutMinutes int
	FlagCommandExecTimeoutSec                 int
}

func (c *Config) setFlags(fs *flag.FlagSet) {
	//common flags
	fs.StringVar(&c.FlagCACertFile, "ca-cert", c.FlagCACertFile, "Use certificate authority.")
	fs.BoolVar(&c.FlagDiag, "diag", c.FlagDiag, "Get diagnostics output once on the CLI. Does not expose API.")
	fs.BoolVar(&c.FlagVerbose, "verbose", c.FlagVerbose, "Use verbose debug output.")
	fs.BoolVar(&c.FlagVersion, "version", c.FlagVersion, "Print version.")
	fs.IntVar(&c.FlagPort, "port", c.FlagPort, "Web server TCP port.")
	fs.IntVar(&c.FlagCommandExecTimeoutSec, "command-exec-timeout", c.FlagCommandExecTimeoutSec,
		"Set command executing timeout")

	// 3dt flags
	fs.BoolVar(&c.FlagPull, "pull", c.FlagPull, "Try to pull checks from DC/OS hosts.")
	fs.IntVar(&c.FlagPullInterval, "pull-interval", c.FlagPullInterval, "Set pull interval in seconds.")
	fs.IntVar(&c.FlagUpdateHealthReportInterval, "health-update-interval", c.FlagUpdateHealthReportInterval,
		"Set update health interval in seconds.")
	fs.StringVar(&c.FlagExhibitorClusterStatusURL, "exhibitor-ip", c.FlagExhibitorClusterStatusURL,
		"Use Exhibitor IP address to discover master nodes.")
	fs.BoolVar(&c.FlagForceTLS, "force-tls", c.FlagForceTLS, "Use HTTPS to do all requests.")


	// snapshot job flags
	fs.StringVar(&c.FlagSnapshotDir, "snapshot-dir", c.FlagSnapshotDir, "Set a path to store snapshots")
	fs.StringVar(&c.FlagSnapshotEndpointsConfigFile, "endpoint-config", c.FlagSnapshotEndpointsConfigFile,
		"Use endpoints_config.json")
	fs.StringVar(&c.FlagSnapshotUnitsLogsSinceHours, "snapshot-units-since", c.FlagSnapshotUnitsLogsSinceHours,
		"Collect systemd units logs since")
	fs.IntVar(&c.FlagSnapshotJobTimeoutMinutes, "snapshot-job-timeout", c.FlagSnapshotJobTimeoutMinutes,
		"Set a global snapshot job timeout")
	fs.IntVar(&c.FlagSnapshotJobGetSingleURLTimeoutMinutes, "snapshot-url-timeout", c.FlagSnapshotJobGetSingleURLTimeoutMinutes,
		"Set a local timeout for every single GET request to a log endpoint")
}

// LoadDefaultConfig sets default config values or sets the values from a command line.
func LoadDefaultConfig(args []string) (config Config, err error) {
	if len(args) == 0 {
		return config, errors.New("arguments cannot be empty")
	}

	// default tcp port is 1050
	config.FlagPort = 1050

	// default pulling and health update interval is 60 seconds
	config.FlagPullInterval = 60
	config.FlagUpdateHealthReportInterval = 60

	config.Version = Version
	config.Revision = Revision

	config.FlagExhibitorClusterStatusURL = "http://127.0.0.1:8181/exhibitor/v1/cluster/status"

	// snapshot job default flag values
	config.FlagSnapshotDir = "/opt/mesosphere/snapshots"
	config.FlagSnapshotJobTimeoutMinutes = 720 //12 hours
	config.FlagSnapshotJobGetSingleURLTimeoutMinutes = 5
	config.FlagCommandExecTimeoutSec = 10

	config.FlagSnapshotEndpointsConfigFile = "/opt/mesosphere/endpoints_config.json"
	config.FlagSnapshotUnitsLogsSinceHours = "24"

	detectIPCmd := os.Getenv("MESOS_IP_DISCOVERY_COMMAND")
	if detectIPCmd == "" {
		detectIPCmd = "/opt/mesosphere/bin/detect_ip"
		log.Warningf("Environment variable MESOS_IP_DISCOVERY_COMMAND is not set, using default location: %s", detectIPCmd)
	}
	config.MesosIPDiscoveryCommand = detectIPCmd

	if os.Getenv("DCOS_VERSION") == "" {
		log.Warning("Environment variable DCOS_VERSION is not set")
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

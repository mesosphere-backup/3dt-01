package api

import (
	"flag"
	"os"

	"errors"
	log "github.com/Sirupsen/logrus"
)

const Version string = "0.1.0"

var Revision string

// config structure used in main
type Config struct {
	Version                         string
	Revision                        string
	MesosIpDiscoveryCommand         string
	DcosVersion                     string
	SystemdUnits                    []string

	FlagPull                        bool
	FlagDiag                        bool
	FlagVerbose                     bool
	FlagVersion                     bool
	FlagPort                        int
	FlagPullInterval                int
	FlagSnapshotDir                 string
	FlagSnapshotEndpointsConfigFile string
	FlagSnapshotUnitsLogsSinceHours string
	FlagSnapshotTimeoutMinutes      int
}

func (c *Config) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.FlagPull, "pull", c.FlagPull, "Try to pull checks from DC/OS hosts.")
	fs.BoolVar(&c.FlagDiag, "diag", c.FlagDiag, "Get diagnostics output once on the CLI. Does not expose API.")
	fs.BoolVar(&c.FlagVerbose, "verbose", c.FlagVerbose, "Use verbose debug output.")
	fs.BoolVar(&c.FlagVersion, "version", c.FlagVersion, "Print version.")
	fs.IntVar(&c.FlagPort, "port", c.FlagPort, "Web server TCP port.")
	fs.IntVar(&c.FlagPullInterval, "pull-interval", c.FlagPullInterval, "Set pull interval, default 60 sec.")
	fs.StringVar(&c.FlagSnapshotDir, "snapshot-dir", c.FlagSnapshotDir, "Set a path to store snapshots.")
	fs.StringVar(&c.FlagSnapshotEndpointsConfigFile, "endpoint-config", c.FlagSnapshotEndpointsConfigFile, "Use endpoints_config.yaml")
	fs.StringVar(&c.FlagSnapshotUnitsLogsSinceHours, "snapshot-units-since", c.FlagSnapshotUnitsLogsSinceHours, "Collect systemd units logs since")
	fs.IntVar(&c.FlagSnapshotTimeoutMinutes, "snapshot-timeout", c.FlagSnapshotTimeoutMinutes, "Set a timeout to collect logs from each node in a cluster, default 10 min.")
}

func LoadDefaultConfig(args []string) (config Config, err error) {
	if len(args) == 0 {
		return config, errors.New("arguments cannot be empty")
	}

	// default tcp port is 1050
	config.FlagPort = 1050

	// default pulling interval is 60 seconds
	config.FlagPullInterval = 60
	config.Version = Version
	config.Revision = Revision

	config.FlagSnapshotDir = "/opt/mesosphere/snapshots"
	config.FlagSnapshotTimeoutMinutes = 720 //12 hours

	config.FlagSnapshotEndpointsConfigFile = "/opt/mesosphere/endpoints_config.json"
	config.FlagSnapshotUnitsLogsSinceHours = "24"

	detectIpCmd := os.Getenv("MESOS_IP_DISCOVERY_COMMAND")
	if detectIpCmd == "" {
		detectIpCmd = "/opt/mesosphere/bin/detect_ip"
		log.Warningf("Environment variable MESOS_IP_DISCOVERY_COMMAND is not set, using default location: %s", detectIpCmd)
	}
	config.MesosIpDiscoveryCommand = detectIpCmd

	if os.Getenv("DCOS_VERSION") == "" {
		log.Warning("Environment variable DCOS_VERSION is not set")
	}
	config.DcosVersion = os.Getenv("DCOS_VERSION")
	config.SystemdUnits = []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}
	flagSet := flag.NewFlagSet("3dt", flag.ContinueOnError)
	config.SetFlags(flagSet)

	// override with user provided arguments
	if err = flagSet.Parse(args[1:]); err != nil {
		return config, err
	}
	return config, nil
}

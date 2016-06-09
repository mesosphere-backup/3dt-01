package api

import (
	"flag"
	"os"

	"errors"
	log "github.com/Sirupsen/logrus"
)

var (
	// Version of 3dt code.
	Version = "0.1.2"

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

	FlagCACertFile                 string
	FlagPull                       bool
	FlagDiag                       bool
	FlagVerbose                    bool
	FlagVersion                    bool
	FlagPort                       int
	FlagPullInterval               int
	FlagUpdateHealthReportInterval int
}

func (c *Config) setFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.FlagCACertFile, "ca-cert", c.FlagCACertFile, "Use certificate authority.")
	fs.BoolVar(&c.FlagPull, "pull", c.FlagPull, "Try to pull checks from DC/OS hosts.")
	fs.BoolVar(&c.FlagDiag, "diag", c.FlagDiag, "Get diagnostics output once on the CLI. Does not expose API.")
	fs.BoolVar(&c.FlagVerbose, "verbose", c.FlagVerbose, "Use verbose debug output.")
	fs.BoolVar(&c.FlagVersion, "version", c.FlagVersion, "Print version.")
	fs.IntVar(&c.FlagPort, "port", c.FlagPort, "Web server TCP port.")
	fs.IntVar(&c.FlagPullInterval, "pull-interval", c.FlagPullInterval, "Set pull interval in seconds.")
	fs.IntVar(&c.FlagUpdateHealthReportInterval, "health-update-interval", c.FlagUpdateHealthReportInterval,
		"Set update health interval in seconds.")
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

	// default CA cert location
	config.FlagCACertFile = "/run/dcos/pki/CA/certs/ca.crt"

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

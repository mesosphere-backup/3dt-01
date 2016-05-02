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
	Version                 string
	Revision                string
	MesosIpDiscoveryCommand string
	DcosVersion             string
	Systemd                 SystemdInterface
	SystemdUnits            []string

	FlagPull         bool
	FlagDiag         bool
	FlagVerbose      bool
	FlagVersion      bool
	FlagPort         int
	FlagPortSsl      int
	FlagSslCertPath  string
	FlagSslKeyPath   string
	FlagPullInterval int
}

func (c *Config) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.FlagPull, "pull", c.FlagPull, "Try to pull checks from DC/OS hosts.")
	fs.BoolVar(&c.FlagDiag, "diag", c.FlagDiag, "Get diagnostics output once on the CLI. Does not expose API.")
	fs.BoolVar(&c.FlagVerbose, "verbose", c.FlagVerbose, "Use verbose debug output.")
	fs.BoolVar(&c.FlagVersion, "version", c.FlagVersion, "Print version.")
	fs.IntVar(&c.FlagPort, "port", c.FlagPort, "HTTP web server port.")
	fs.IntVar(&c.FlagPortSsl, "port-ssl", c.FlagPortSsl, "HTTPS web server port.")
	fs.StringVar(&c.FlagSslCertPath, "cert", c.FlagSslCertPath, "SSL certificate path.")
	fs.StringVar(&c.FlagSslKeyPath, "key", c.FlagSslKeyPath, "SSL key path.")
	fs.IntVar(&c.FlagPullInterval, "pull-interval", c.FlagPullInterval, "Set pull interval, default 60 sec.")
}

func LoadDefaultConfig(args []string) (config Config, err error) {
	if len(args) == 0 {
		return config, errors.New("arguments cannot be empty")
	}

	// default http port
	config.FlagPort = 1050

	// default https port
	config.FlagPortSsl = 1051

	// TODO(mnaboka): FIX ME
	// default cert / key path
	config.FlagSslCertPath = "/opt/mesosphere/ssl/cert.pem"
	config.FlagSslKeyPath = "/opt/mesosphere/ssl/key.pem"

	// default pulling interval is 60 seconds
	config.FlagPullInterval = 60
	config.Version = Version
	config.Revision = Revision

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
	config.Systemd = &SystemdType{}
	config.SystemdUnits = []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}

	flagSet := flag.NewFlagSet("3dt", flag.ContinueOnError)
	config.SetFlags(flagSet)

	// override with user provided arguments
	if err = flagSet.Parse(args[1:]); err != nil {
		return config, err
	}
	return config, nil
}

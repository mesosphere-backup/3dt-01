package api

import (
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/xeipuuv/gojsonschema"
)

var (
	// Version of 3dt code.
	Version = "0.3.0"

	// APIVer is an API version.
	APIVer = 1

	// flagSet
	flagSet = flag.NewFlagSet("3dt", flag.ContinueOnError)

	internalJSONValidationSchema = `
	{
	  "title": "User config validate schema",
	  "type": "object",
	  "properties": {
	    "ca-cert": {
	      "type": "string"
	    },
	    "port": {
	      "type": "integer",
	      "minimum": 1024,
	      "maximum": 65535
	    },
	    "pull": {
	      "type": "boolean"
	    },
	    "master-port": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 65535
	    },
	    "agent-port": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 65535
	    },
	    "pull-interval": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 3600
	    },
	    "verbose": {
	      "type": "boolean"
	    },
	    "health-update-interval": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 3600
	    },
	    "exhibitor-ip": {
	      "type": "string"
	    },
	    "diagnostics-job-timeout": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 720
	    },
	    "pull-timeout": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 60
	    },
	    "force-tls": {
	      "type": "boolean"
	    },
	    "command-exec-timeout": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 480
	    },
	    "diagnostics-units-since": {
	      "type": "string"
	    },
	    "diagnostics-url-timeout": {
	      "type": "integer",
	      "minimum": 1,
	      "maximum": 60
	    },
	    "diagnostics-bundle-dir": {
	      "type": "string"
	    },
	    "endpoint-config": {
	      "type": "string"
	    },
	    "json-validation-schema": {
	      "type": "string"
	    },
	    "iam-config": {
	      "type": "string"
	    },
	    "debug": {
	      "type": "boolean"
	    },
	    "role": {
	      "type": "string",
	      "enum": ["master", "agent", "agent_public", ""]
	    }
	  },
	  "additionalProperties": false
	}`
)

// Config structure is a main config object
type Config struct {
	Version                 string   `json:"-"`
	MesosIPDiscoveryCommand string   `json:"-"`
	DCOSVersion             string   `json:"-"`
	SystemdUnits            []string `json:"-"`

	// config flag
	Flag3DTConfig  string `json:"-"`
	FlagJSONSchema string `json:"json-validation-schema"`

	// 3dt flags
	FlagCACertFile                 string `json:"ca-cert"`
	FlagPull                       bool   `json:"pull"`
	FlagDiag                       bool   `json:"-"`
	FlagVerbose                    bool   `json:"verbose"`
	FlagVersion                    bool   `json:"-"`
	FlagPort                       int    `json:"port"`
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

func (c *Config) setFlags(fs *flag.FlagSet) {
	// config flag
	fs.StringVar(&c.Flag3DTConfig, "3dt-config", c.Flag3DTConfig, "Use 3DT config file.")
	fs.StringVar(&c.FlagJSONSchema, "json-validation-schema", c.FlagJSONSchema, "Path to JSON validation schema.")

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
	fs.BoolVar(&c.FlagDebug, "debug", c.FlagDebug, "Enable pprof debugging endpoints.")
	fs.StringVar(&c.FlagRole, "role", c.FlagRole, "Set node role")
	fs.StringVar(&c.FlagIAMConfig, "iam-config", c.FlagIAMConfig, "A path to identity and access managment config")

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

	config.FlagExhibitorClusterStatusURL = "http://127.0.0.1:8181/exhibitor/v1/cluster/status"

	// diagnostics job default flag values
	config.FlagDiagnosticsBundleDir = "/var/run/dcos/3dt/diagnostic_bundles"
	config.FlagDiagnosticsJobTimeoutMinutes = 720 //12 hours

	// 2 minutes for a URL GET timeout.
	config.FlagDiagnosticsJobGetSingleURLTimeoutMinutes = 2

	// 2 minutes for a command to run
	config.FlagCommandExecTimeoutSec = 120

	config.FlagDiagnosticsBundleEndpointsConfigFile = "/opt/mesosphere/etc/endpoints_config.json"
	config.FlagDiagnosticsBundleUnitsLogsSinceString = "24h"

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

	config.setFlags(flagSet)

	// override with user provided arguments
	if err := flagSet.Parse(args[1:]); err != nil {
		return config, err
	}

	// check for provided JSON validation schema
	if config.FlagJSONSchema != "" {
		validationSchema, err := ioutil.ReadFile(config.FlagJSONSchema)
		if err != nil {
			return config, err
		}
		internalJSONValidationSchema = string(validationSchema)
	}

	// if config passed, read it and override the default values with values in config
	if config.Flag3DTConfig != "" {
		return readConfigFile(config.Flag3DTConfig, config)
	}

	//validate the config structure
	if err := validateConfigStruct(config); err != nil {
		return config, err
	}
	return config, nil
}

func readConfigFile(configPath string, defaultConfig Config) (Config, error) {
	configContent, err := ioutil.ReadFile(configPath)
	if err != nil {
		return defaultConfig, err
	}

	if err := validateConfigFile(configContent); err != nil {
		return defaultConfig, err
	}

	// override default values
	if err := json.Unmarshal(configContent, &defaultConfig); err != nil {
		return defaultConfig, err
	}

	// validate the result of overriding the default config with values from a config file
	return defaultConfig, validateConfigStruct(defaultConfig)
}

func validate(documentLoader gojsonschema.JSONLoader) error {
	schemaLoader := gojsonschema.NewStringLoader(internalJSONValidationSchema)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		return err
	}
	result, err := schema.Validate(documentLoader)
	if err != nil {
		return err
	}

	if !result.Valid() {
		return printErrorsAndFail(result.Errors())
	}
	return nil
}

func printErrorsAndFail(resultErrors []gojsonschema.ResultError) error {
	for _, resultError := range resultErrors {
		log.Error(resultError)
	}
	return errors.New("Validation failed")
}

func validateConfigStruct(config Config) error {
	documentLoader := gojsonschema.NewGoLoader(config)
	return validate(documentLoader)
}

func validateConfigFile(configContent []byte) error {
	documentLoader := gojsonschema.NewStringLoader(string(configContent))
	return validate(documentLoader)
}

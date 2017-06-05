// Copyright © 2017 Mesosphere Inc. <http://mesosphere.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/activation"
	"github.com/dcos/3dt/api"
	"github.com/dcos/dcos-go/dcos"
	"github.com/dcos/dcos-go/dcos/http/transport"
	"github.com/dcos/dcos-go/dcos/nodeutil"
	"github.com/spf13/cobra"
)

const (
	diagnosticsTCPPort        = 1050
	diagnosticsBundleDir      = "/var/run/dcos/3dt/diagnostic_bundles"
	diagnosticsEndpointConfig = "/opt/mesosphere/etc/endpoints_config.json"
	exhibitorURL              = "http://127.0.0.1:8181/exhibitor/v1/cluster/status"
	defaultCheckConfig        = "/opt/mesosphere/etc/dcos-check-config.json"
)

// override the defaultStateURL to use https scheme
var defaultStateURL = url.URL{
	Scheme: "https",
	Host:   net.JoinHostPort(dcos.DNSRecordLeader, strconv.Itoa(dcos.PortMesosMaster)),
	Path:   "/state",
}

// daemonCmd represents the daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		startDiagnosticsDaemon()
	},
}

func init() {
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagCACertFile, "ca-cert", defaultConfig.FlagCACertFile,
		"Use certificate authority.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagPort, "port", diagnosticsTCPPort,
		"Web server TCP port.")
	daemonCmd.PersistentFlags().BoolVar(&defaultConfig.FlagDisableUnixSocket, "no-unix-socket",
		defaultConfig.FlagDisableUnixSocket, "Disable use unix socket provided by systemd activation.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagMasterPort, "master-port", diagnosticsTCPPort,
		"Use TCP port to connect to masters.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagAgentPort, "agent-port", diagnosticsTCPPort,
		"Use TCP port to connect to agents.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagCommandExecTimeoutSec, "command-exec-timeout",
		120, "Set command executing timeout")
	daemonCmd.PersistentFlags().BoolVar(&defaultConfig.FlagPull, "pull", defaultConfig.FlagPull,
		"Try to pull runner from DC/OS hosts.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagPullInterval, "pull-interval", 60,
		"Set pull interval in seconds.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagPullTimeoutSec, "pull-timeout", 3,
		"Set pull timeout.")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagUpdateHealthReportInterval, "health-update-interval",
		60,
		"Set update health interval in seconds.")
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagExhibitorClusterStatusURL, "exhibitor-url", exhibitorURL,
		"Use Exhibitor URL to discover master nodes.")
	daemonCmd.PersistentFlags().BoolVar(&defaultConfig.FlagForceTLS, "force-tls", defaultConfig.FlagForceTLS,
		"Use HTTPS to do all requests.")
	daemonCmd.PersistentFlags().BoolVar(&defaultConfig.FlagDebug, "debug", defaultConfig.FlagDebug,
		"Enable pprof debugging endpoints.")
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagIAMConfig, "iam-config",
		defaultConfig.FlagIAMConfig, "A path to identity and access managment config")
	// diagnostics job flags
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagDiagnosticsBundleDir,
		"diagnostics-bundle-dir", diagnosticsBundleDir, "Set a path to store diagnostic bundles")
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagDiagnosticsBundleEndpointsConfigFile,
		"endpoint-config", diagnosticsEndpointConfig,
		"Use endpoints_config.json")
	daemonCmd.PersistentFlags().StringVar(&defaultConfig.FlagDiagnosticsBundleUnitsLogsSinceString,
		"diagnostics-units-since", "24h", "Collect systemd units logs since")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagDiagnosticsJobTimeoutMinutes,
		"diagnostics-job-timeout", 720,
		"Set a global diagnostics job timeout")
	daemonCmd.PersistentFlags().IntVar(&defaultConfig.FlagDiagnosticsJobGetSingleURLTimeoutMinutes,
		"diagnostics-url-timeout", 2,
		"Set a local timeout for every single GET request to a log endpoint")
	RootCmd.AddCommand(daemonCmd)
}

func startDiagnosticsDaemon() {
	// init new transport
	transportOptions := []transport.OptionTransportFunc{}
	if defaultConfig.FlagCACertFile != "" {
		transportOptions = append(transportOptions, transport.OptionCaCertificatePath(defaultConfig.FlagCACertFile))
	}
	if defaultConfig.FlagIAMConfig != "" {
		transportOptions = append(transportOptions, transport.OptionIAMConfigPath(defaultConfig.FlagIAMConfig))
	}

	tr, err := transport.NewTransport(transportOptions...)
	if err != nil {
		logrus.Fatalf("Unable to initialize HTTP transport: %s", err)
	}

	client := &http.Client{
		Transport: tr,
	}

	var options []nodeutil.Option
	if defaultConfig.FlagForceTLS {
		options = append(options, nodeutil.OptionMesosStateURL(defaultStateURL.String()))
	}
	nodeInfo, err := nodeutil.NewNodeInfo(client, defaultConfig.FlagRole, options...)
	if err != nil {
		logrus.Fatalf("Could not initialize nodeInfo: %s", err)
	}

	DCOSTools := &api.DCOSTools{
		ExhibitorURL: defaultConfig.FlagExhibitorClusterStatusURL,
		ForceTLS:     defaultConfig.FlagForceTLS,
		Role:         defaultConfig.FlagRole,
		NodeInfo:     nodeInfo,
		Transport:    tr,
	}

	// Create and init diagnostics job, do not hard fail on error
	diagnosticsJob := &api.DiagnosticsJob{
		Transport: tr,
	}
	if err := diagnosticsJob.Init(defaultConfig, DCOSTools); err != nil {
		logrus.Fatalf("Could not init diagnostics job properly: %s", err)
	}

	// Inject dependencies used for running 3dt.
	dt := &api.Dt{
		Cfg:               defaultConfig,
		DtDCOSTools:       DCOSTools,
		DtDiagnosticsJob:  diagnosticsJob,
		RunPullerChan:     make(chan bool),
		RunPullerDoneChan: make(chan bool),
		SystemdUnits:      &api.SystemdUnits{},
		MR:                &api.MonitoringResponse{},
	}

	// start diagnostic server and expose endpoints.
	logrus.Info("Start 3DT")

	// start pulling every 60 seconds.
	if defaultConfig.FlagPull {
		go api.StartPullWithInterval(dt)
	}

	router := api.NewRouter(dt)

	if defaultConfig.FlagDisableUnixSocket {
		logrus.Infof("Exposing 3DT API on 0.0.0.0:%d", defaultConfig.FlagPort)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", defaultConfig.FlagPort), router))
	}

	// try using systemd socket
	listeners, err := activation.Listeners(true)
	if err != nil {
		logrus.Fatalf("Unable to initialize listener: %s", err)
	}

	if len(listeners) == 0 || listeners[0] == nil {
		logrus.Fatal("Unix socket not found")
	}
	logrus.Infof("Using socket: %s", listeners[0].Addr().String())
	logrus.Fatal(http.Serve(listeners[0], router))
}

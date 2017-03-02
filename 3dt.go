package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/activation"
	"github.com/dcos/3dt/api"
	"github.com/dcos/dcos-go/dcos"
	"github.com/dcos/dcos-go/dcos/http/transport"
	"github.com/dcos/dcos-go/dcos/nodeutil"
)

// override the defaultStateURL to use https scheme
var defaultStateURL = url.URL{
	Scheme: "https",
	Host:   net.JoinHostPort(dcos.DNSRecordLeader, strconv.Itoa(dcos.PortMesosMaster)),
	Path:   "/state",
}

func getVersion() string {
	return fmt.Sprintf("Version: %s", api.Version)
}

func runDiag(dt *api.Dt) {
	units, err := dt.SystemdUnits.GetUnitsProperties(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		logrus.Fatalf("Error getting units properties: %s", err)
	}

	var fail bool
	for _, unit := range units.Array {
		if unit.UnitHealth != 0 {
			fmt.Printf("[%s]: %s %s\n", unit.UnitID, unit.UnitTitle, unit.UnitOutput)
			fail = true
		}
	}

	if fail {
		logrus.Fatal("Found unhealthy systemd units")
	}
}

func main() {
	// load config with default values
	config, err := api.LoadDefaultConfig(os.Args)
	if err != nil {
		logrus.Fatalf("Could not load default config: %s", err)
	}

	// print version and exit
	if config.FlagVersion {
		fmt.Println(getVersion())
		os.Exit(0)
	}

	// init new transport
	transportOptions := []transport.OptionTransportFunc{}
	if config.FlagCACertFile != "" {
		transportOptions = append(transportOptions, transport.OptionCaCertificatePath(config.FlagCACertFile))
	}
	if config.FlagIAMConfig != "" {
		transportOptions = append(transportOptions, transport.OptionIAMConfigPath(config.FlagIAMConfig))
	}

	tr, err := transport.NewTransport(transportOptions...)
	if err != nil {
		logrus.Fatalf("Unable to initialize HTTP transport: %s", err)
	}

	client := &http.Client{
		Transport: tr,
	}

	var options []nodeutil.Option
	if config.FlagForceTLS {
		options = append(options, nodeutil.OptionMesosStateURL(defaultStateURL.String()))
	}
	nodeInfo, err := nodeutil.NewNodeInfo(client, config.FlagRole, options...)
	if err != nil {
		logrus.Fatalf("Could not initialize nodeInfo: %s", err)
	}

	DCOSTools := &api.DCOSTools{
		ExhibitorURL: config.FlagExhibitorClusterStatusURL,
		ForceTLS:     config.FlagForceTLS,
		Role:         config.FlagRole,
		NodeInfo:     nodeInfo,
		Transport:    tr,
	}

	// Create and init diagnostics job, do not hard fail on error
	diagnosticsJob := &api.DiagnosticsJob{
		Transport: tr,
	}
	if err := diagnosticsJob.Init(config, DCOSTools); err != nil {
		logrus.Fatalf("Could not init diagnostics job properly: %s", err)
	}

	// Inject dependencies used for running 3dt.
	dt := &api.Dt{
		Cfg:               config,
		DtDCOSTools:       DCOSTools,
		DtDiagnosticsJob:  diagnosticsJob,
		RunPullerChan:     make(chan bool),
		RunPullerDoneChan: make(chan bool),
		SystemdUnits:      &api.SystemdUnits{},
		MR:                &api.MonitoringResponse{},
	}

	// set verbose (debug) output.
	if config.FlagVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// run local diagnostics, verify all systemd units are healthy.
	if config.FlagDiag {
		runDiag(dt)
		os.Exit(0)
	}

	// start diagnostic server and expose endpoints.
	logrus.Info("Start 3DT")

	// start pulling every 60 seconds.
	if config.FlagPull {
		go api.StartPullWithInterval(dt)
	}

	router := api.NewRouter(dt)

	if config.FlagDisableUnixSocket {
		logrus.Infof("Exposing 3DT API on 0.0.0.0:%d", config.FlagPort)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.FlagPort), router))
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

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
	"github.com/dcos/3dt/config"
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
	return fmt.Sprintf("Version: %s", config.Version)
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
	// load cfg with default values
	cfg, err := config.LoadDefaultConfig(os.Args)
	if err != nil {
		logrus.Fatalf("Could not load default cfg: %s", err)
	}

	// print version and exit
	if cfg.FlagVersion {
		fmt.Println(getVersion())
		os.Exit(0)
	}

	// init new transport
	transportOptions := []transport.OptionTransportFunc{}
	if cfg.FlagCACertFile != "" {
		transportOptions = append(transportOptions, transport.OptionCaCertificatePath(cfg.FlagCACertFile))
	}
	if cfg.FlagIAMConfig != "" {
		transportOptions = append(transportOptions, transport.OptionIAMConfigPath(cfg.FlagIAMConfig))
	}

	tr, err := transport.NewTransport(transportOptions...)
	if err != nil {
		logrus.Fatalf("Unable to initialize HTTP transport: %s", err)
	}

	client := &http.Client{
		Transport: tr,
	}

	var options []nodeutil.Option
	if cfg.FlagForceTLS {
		options = append(options, nodeutil.OptionMesosStateURL(defaultStateURL.String()))
	}
	nodeInfo, err := nodeutil.NewNodeInfo(client, cfg.FlagRole, options...)
	if err != nil {
		logrus.Fatalf("Could not initialize nodeInfo: %s", err)
	}

	DCOSTools := &api.DCOSTools{
		ExhibitorURL: cfg.FlagExhibitorClusterStatusURL,
		ForceTLS:     cfg.FlagForceTLS,
		Role:         cfg.FlagRole,
		NodeInfo:     nodeInfo,
		Transport:    tr,
	}

	// Create and init diagnostics job, do not hard fail on error
	diagnosticsJob := &api.DiagnosticsJob{
		Transport: tr,
	}
	if err := diagnosticsJob.Init(cfg, DCOSTools); err != nil {
		logrus.Fatalf("Could not init diagnostics job properly: %s", err)
	}

	// Inject dependencies used for running 3dt.
	dt := &api.Dt{
		Cfg:               cfg,
		DtDCOSTools:       DCOSTools,
		DtDiagnosticsJob:  diagnosticsJob,
		RunPullerChan:     make(chan bool),
		RunPullerDoneChan: make(chan bool),
		SystemdUnits:      &api.SystemdUnits{},
		MR:                &api.MonitoringResponse{},
	}

	// set verbose (debug) output.
	if cfg.FlagVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// run local diagnostics, verify all systemd units are healthy.
	if cfg.FlagDiag {
		runDiag(dt)
		os.Exit(0)
	}

	// start diagnostic server and expose endpoints.
	logrus.Info("Start 3DT")

	// start pulling every 60 seconds.
	if cfg.FlagPull {
		go api.StartPullWithInterval(dt)
	}

	router := api.NewRouter(dt)

	if cfg.FlagDisableUnixSocket {
		logrus.Infof("Exposing 3DT API on 0.0.0.0:%d", cfg.FlagPort)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.FlagPort), router))
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

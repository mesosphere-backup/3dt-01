package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/activation"
	"github.com/dcos/3dt/api"
	"github.com/dcos/dcos-go/dcos/nodeutil"
)

func getVersion() string {
	return fmt.Sprintf("Version: %s, Revision: %s", api.Version, api.Revision)
}

func runDiag(dt api.Dt) {
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

	client := &http.Client{}
	nodeInfo, err := nodeutil.NewNodeInfo(client)
	if err != nil {
		logrus.Fatalf("Could not initialize nodeInfo: %s", err)
	}

	DCOSTools := &api.DCOSTools{
		ExhibitorURL: config.FlagExhibitorClusterStatusURL,
		ForceTLS:     config.FlagForceTLS,
		Role: config.FlagRole,
		NodeInfo: nodeInfo,
	}

	// init requester
	if err := api.Requester.Init(&config, DCOSTools); err != nil {
		logrus.Fatalf("Could not initialze the HTTP(S) requester: %s", err)
	}

	// Create and init diagnostics job, do not hard fail on error
	diagnosticsJob := &api.DiagnosticsJob{}
	if err := diagnosticsJob.Init(&config, DCOSTools); err != nil {
		logrus.Errorf("Could not init diagnostics job properly: %s", err)
	}

	// Inject dependencies used for running 3dt.
	dt := api.Dt{
		Cfg:               &config,
		DtDCOSTools:       DCOSTools,
		DtDiagnosticsJob:  diagnosticsJob,
		RunPullerChan:     make(chan bool),
		RunPullerDoneChan: make(chan bool),
		SystemdUnits:      &api.SystemdUnits{},
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

	// try using systemd socket
	listeners, err := activation.Listeners(true)
	if err != nil {
		logrus.Errorf("Systemd socket not found: %s", err)
	}

	if len(listeners) == 0 || listeners[0] == nil {
		logrus.Infof("Exposing 3DT API on 0.0.0.0:%d", config.FlagPort)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.FlagPort), router))
	}
	logrus.Infof("Using socket: %s", listeners[0].Addr().String())
	logrus.Fatal(http.Serve(listeners[0], router))
}

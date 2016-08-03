package main

import (
	"fmt"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/activation"
	"github.com/dcos/3dt/api"
	"net/http"
)

func getVersion() string {
	return (fmt.Sprintf("Version: %s, Revision: %s", api.Version, api.Revision))
}

func runDiag(dt api.Dt) {
	units, err := api.GetUnitsProperties(dt)
	if err != nil {
		log.Fatalf("Error getting units properties: %s", err)
	}

	var fail bool
	for _, unit := range units.Array {
		if unit.UnitHealth != 0 {
			fmt.Printf("[%s]: %s %s\n", unit.UnitID, unit.UnitTitle, unit.UnitOutput)
			fail = true
		}
	}

	if fail {
		log.Fatal("Found unhealthy systemd units")
	}
}

func main() {
	// a message channel to ensure we can start pulling safely
	readyChan := make(chan struct{})

	// load config with default values
	config, err := api.LoadDefaultConfig(os.Args)
	if err != nil {
		log.Fatalf("Couuld not load default config: %s", err)
	}

	// print version and exit
	if config.FlagVersion {
		fmt.Println(getVersion())
		os.Exit(0)
	}

	DCOSTools := &api.DCOSTools{
		ExhibitorURL: config.FlagExhibitorClusterStatusURL,
		ForceTLS:     config.FlagForceTLS,
	}

	// init requester
	if err := api.Requester.Init(&config, DCOSTools); err != nil {
		log.Fatalf("Could not initialze the HTTP(S) requester: %s", err)
	}

	// Create and init diagnostics job, do not hard fail on error
	diagnosticsJob := &api.DiagnosticsJob{}
	if err := diagnosticsJob.Init(&config, DCOSTools); err != nil {
		log.Errorf("Could not init diagnostics job properly: %s", err)
	}

	// Inject dependencies used for running 3dt.
	dt := api.Dt{
		Cfg:              &config,
		DtDCOSTools:      DCOSTools,
		DtDiagnosticsJob: diagnosticsJob,
	}

	// set verbose (debug) output.
	if config.FlagVerbose {
		log.SetLevel(log.DebugLevel)
	}

	// run local diagnostics, verify all systemd units are healthy.
	if config.FlagDiag {
		runDiag(dt)
		os.Exit(0)
	}

	// start pulling every 60 seconds.
	if config.FlagPull {
		go api.StartPullWithInterval(dt, readyChan)
	}

	// start diagnostic server and expose endpoints.
	log.Info("Start 3DT")
	go api.StartUpdateHealthReport(dt, readyChan, false)
	router := api.NewRouter(dt)

	// try using systemd socket
	listeners, err := activation.Listeners(true)
	if err != nil {
		log.Errorf("Systemd socket not found: %s", err)
	}

	if len(listeners) == 0 || listeners[0] == nil {
		log.Infof("Exposing 3DT API on 0.0.0.0:%d", config.FlagPort)
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.FlagPort), router))
	}
	log.Infof("Using socket: %s", listeners[0].Addr().String())
	log.Fatal(http.Serve(listeners[0], router))
}

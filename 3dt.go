package main

import (
	"fmt"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/api"
	"net/http"
)

func getVersion() string {
	return (fmt.Sprintf("Version: %s, Revision: %s", api.Version, api.Revision))
}

func runDiag(dt api.Dt) int {
	var exitCode int
	units, err := api.GetUnitsProperties(dt)
	if err != nil {
		log.Error(err)
		return 1
	}
	for _, unit := range units.Array {
		if unit.UnitHealth != 0 {
			fmt.Printf("[%s]: %s %s\n", unit.UnitID, unit.UnitTitle, unit.UnitOutput)
			exitCode = 1
		}
	}
	return exitCode
}

func main() {
	// a message channel to ensure we can start pulling safely
	readyChan := make(chan struct{})

	// load config with default values
	config, err := api.LoadDefaultConfig(os.Args)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	// print version and exit
	if config.FlagVersion {
		fmt.Println(getVersion())
		os.Exit(0)
	}

	// Inject dependencies used for running 3dt.
	dt := api.Dt{
		Cfg: &config,
		DtPuller: &api.DcosPuller{},
		DtDCOSTools: &api.DCOSTools{},
	}

	// run local diagnostics, verify all systemd units are healthy.
	if config.FlagDiag {
		os.Exit(runDiag(dt))
	}

	// set verbose (debug) output.
	if config.FlagVerbose {
		log.SetLevel(log.DebugLevel)
	}

	// start pulling every 60 seconds.
	if config.FlagPull {
		go api.StartPullWithInterval(dt, readyChan)
	}

	// start diagnostic server and expose endpoints.
	log.Info("Start 3DT")
	go api.StartUpdateHealthReport(dt, readyChan, false)
	router := api.NewRouter(dt)
	log.Infof("Exposing 3DT API on 0.0.0.0:%d", config.FlagPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.FlagPort), router))
}

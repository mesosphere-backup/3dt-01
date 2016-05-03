package main

import (
	"fmt"
	"os"

	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/api"
	"github.com/gorilla/mux"
	"net/http"
	"strings"
)

func getVersion() string {
	return (fmt.Sprintf("Version: %s, Revision: %s", api.Version, api.Revision))
}

func runDiag(config api.Config) int {
	var exitCode int = 0
	units, err := api.GetUnitsProperties(&config)
	if err != nil {
		log.Error(err)
		return 1
	}
	for _, unit := range units.Array {
		if unit.UnitHealth != 0 {
			fmt.Printf("[%s]: %s %s\n", unit.UnitId, unit.UnitTitle, unit.UnitOutput)
			exitCode = 1
		}
	}
	return exitCode
}

func doFilesExist(files []string) error {
	var errs []string

	for _, file := range files {
		_, err := os.Stat(file)
		if err != nil && os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func runHttpServer(addr string, sslAddr string, certPath string, keyPath string, router *mux.Router) chan error {
	errs := make(chan error)

	// Start Http server
	go func() {
		log.Infof("Exposing HTTP 3DT API on %s", addr)
		if err := http.ListenAndServe(addr, router); err != nil {
			errs <- err
		}
	}()

	if err := doFilesExist([]string{certPath, keyPath}); err != nil {
		log.Error("Could not start HTTPS server")
		log.Error(err)
	} else {
		// Start Https server
		go func() {
			log.Infof("Exposing HTTPS 3DT API on %s", sslAddr)
			if err := http.ListenAndServeTLS(sslAddr, certPath, keyPath, router); err != nil {
				// do not fail if certificates did not load properly.
				log.Error(err)
			}
		}()
	}

	return errs
}

func main() {
	// a message channel to ensure we can start pulling safely
	readyChan := make(chan bool, 1)

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

	// run local diagnostics, verify all systemd units are healthy.
	if config.FlagDiag {
		os.Exit(runDiag(config))
	}

	// set verbose (debug) output.
	if config.FlagVerbose {
		log.SetLevel(log.DebugLevel)
	}

	// start pulling every 60 seconds.
	if config.FlagPull {
		puller := api.PullType{}
		go api.StartPullWithInterval(config, &puller, readyChan)
	}

	// start diagnostic server and expose endpoints.
	log.Info("Start 3DT")
	go api.StartUpdateHealthReport(config, readyChan, false)
	router := api.NewRouter(&config)
	errs := runHttpServer(fmt.Sprintf(":%d", config.FlagPort), fmt.Sprintf(":%d", config.FlagPortSsl),
		config.FlagSslCertPath, config.FlagSslKeyPath, router)

	select {
	case err := <-errs:
		log.Fatal(err)
	}
}

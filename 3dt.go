package main

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/api"
	"github.com/dcos/3dt/cmd"
)

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
	cmd.Execute()
}

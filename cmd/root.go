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
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/api"
	"github.com/dcos/3dt/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	version       bool
	diag          bool
	cfgFile       string
	defaultConfig *config.Config = &config.Config{}
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "3dt",
	Short: "DC/OS diagnostics service",
	Long: `DC/OS diagnostics service provides health information about cluster.

3dt daemon start an http server and polls the components health.
3dt check provides CLI functionality to run checks on DC/OS cluster.
`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {
		if version {
			fmt.Printf("Version: %s\n", config.Version)
			os.Exit(0)
		}

		if diag {
			os.Exit(runDiag())
		}
		cmd.Help()
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().BoolVar(&version, "version", false, "Print 3dt version")
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.3dt.yaml)")
	RootCmd.PersistentFlags().BoolVar(&diag, "diag", false,
		"Check DC/OS components health.")
	RootCmd.PersistentFlags().BoolVar(&defaultConfig.FlagVerbose, "verbose", defaultConfig.FlagVerbose,
		"Use verbose debug output.")
	RootCmd.PersistentFlags().StringVar(&defaultConfig.FlagRole, "role", defaultConfig.FlagRole,
		"Set node role")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.SetConfigName("dcos-3dt-config") // name of config file (without extension)
	viper.AddConfigPath("/opt/mesosphere/etc/")
	viper.AutomaticEnv()

	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		if err := defaultConfig.LoadFromViper(viper.AllSettings()); err != nil {
			logrus.Fatalf("Error loading config file: %s", err)
		}
	}
}


func runDiag() int {
	sdu := &api.SystemdUnits{}
	units, err := sdu.GetUnits(&api.DCOSTools{})
	if err != nil {
		logrus.Errorf("Error getting units properties: %s", err)
		return 1
	}

	var fail bool
	for _, unit := range units {
		if unit.UnitHealth != 0 {
			fmt.Printf("[%s]: %s %s\n", unit.UnitID, unit.UnitTitle, unit.UnitOutput)
			fail = true
		}
	}

	if fail {
		logrus.Error("Found unhealthy systemd units")
		return 1
	}

	return 0
}

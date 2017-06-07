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
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/runner"
	"github.com/spf13/cobra"
)

const (
	checkTypeCluster       = "cluster"
	checkTypeNodePreStart  = "node-prestart"
	checkTypeNodePostStart = "node-poststart"

	defaultRunnerConfig    = "/opt/mesosphere/etc/dcos-3dt-runner-config.json"
)

var (
	list          bool
	checksCfgFile string
)

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check <check-type>",
	Short: "Execute a DC/OS check",
	Long: `A DC/OS check can be one of the following types: cluster, node-prestart, node-poststart`,
	Run: func(cmd *cobra.Command, args []string) {
		var selectiveChecks []string
		if len(args) == 0 {
			cmd.Usage()
			return
		} else if len(args) > 1 {
			selectiveChecks = args[1:]
		}

		r := runner.NewRunner(defaultConfig.FlagRole)
		if err := r.LoadFromFile(checksCfgFile); err != nil {
			logrus.Fatal(err)
		}

		var (
			rs  *runner.CombinedResponse
			err error
		)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		switch args[0] {
		case checkTypeCluster:
			rs, err = r.Cluster(ctx, list, selectiveChecks...)
			if err != nil {
				logrus.Fatalf("unable to execute cluster checks: %s", err)
			}
		case checkTypeNodePreStart:
			rs, err = r.PreStart(ctx, list, selectiveChecks...)
			if err != nil {
				logrus.Fatalf("unable to execute prestart checks: %s", err)
			}
		case checkTypeNodePostStart:
			rs, err = r.PostStart(ctx, list, selectiveChecks...)
			if err != nil {
				logrus.Fatalf("unable to execute poststart checks: %s", err)
			}
		default:
			logrus.Fatalf("invalid check type %s", args[0])
		}

		os.Exit(emitOutput(rs))
	},
}

func init() {
	RootCmd.AddCommand(checkCmd)
	checkCmd.PersistentFlags().BoolVar(&list, "list", false, "List runner")
	checkCmd.PersistentFlags().StringVar(&checksCfgFile, "check-config", defaultRunnerConfig,
		"Path to dcos-check config file")
}

func emitOutput(rc *runner.CombinedResponse) int {
	body, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		logrus.Fatal(err)
	}
	fmt.Println(string(body))
	return rc.Status()
}

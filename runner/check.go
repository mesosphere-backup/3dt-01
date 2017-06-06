package runner

import (
	"context"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/dcos-go/exec"
	"github.com/pkg/errors"
)

// Check is a basic structure that describes DC/OS check.
type Check struct {
	// Cmd is a path to executable (script or binary) and arguments
	Cmd []string `json:"cmd"`

	// Description provides a basic check description.
	Description string `json:"description"`

	// Timeout is defines a custom script timeout.
	Timeout string `json:"timeout"`

	// Roles is a list of DC/OS roles (e.g. master, agent). Node must be one of roles
	// to execute a check.
	Roles []string `json:"roles"`
}

// Run executes the given check.
func (c *Check) Run(ctx context.Context, role string) ([]byte, int, error) {
	if !c.verifyRole(role) {
		return nil, -1, errors.Errorf("check can be executed on a node with the following roles %s. Current role %s", c.Roles, role)
	}

	if len(c.Cmd) == 0 {
		return nil, -1, errors.New("unable to execute a command with empty Cmd field")
	}

	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		logrus.Errorf("error reading timeout %s. Using default timeout 5sec", err)
		timeout = time.Second*5
	}

	newCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout, stderr, code, err := exec.Output(newCtx, c.Cmd...)
	if err != nil {
		return nil, -1, err
	}

	combinedOutput := append(stdout, stderr...)
	return combinedOutput, code, nil
}

func (c *Check) verifyRole(role string) bool {
	// no roles means we are allowed to execute a check on any node.
	if len(c.Roles) == 0 {
		return true
	}

	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

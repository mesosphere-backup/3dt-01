package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

func TestConfigLoadConfig(t *testing.T) {
	c := &Runner{}
	if err := c.LoadFromFile("./fixture/checks.json"); err != nil {
		t.Fatal(err)
	}
}

func TestNewRunner(t *testing.T) {
	r := NewRunner("master")
	if r.role != "master" {
		t.Fatalf("expecting role master. Got %s", r.role)
	}

	r = NewRunner("agent")
	if r.role != "agent" {
		t.Fatalf("expecting role agent. Got %s", r.role)
	}

	r = NewRunner("agent_public")
	if r.role != "agent" {
		t.Fatalf("expecting role agent. Got %s", r.role)
	}
}

func TestRun(t *testing.T) {
	r := NewRunner("master")
	cfg := `
	{
	  "cluster_checks": {
	    "test_check": {
	      "cmd": ["./fixture/combined.sh"],
	      "timeout": "1s"
	    }
	  },
	  "node_checks": {
	    "checks": {
	      "check1": {
	        "cmd": ["./fixture/combined.sh"],
		  "timeout": "1s"
	        },
	      "check2": {
		"cmd": ["./fixture/combined.sh"],
		"timeout": "1s"
	      }
	    },
	    "prestart": ["check1"],
	    "poststart": ["check2"]
	   }
	}
	`
	r.Load(strings.NewReader(cfg))
	out, err := r.Cluster(context.TODO(), false)
	if err != nil {
		t.Fatal(err)
	}

	expectedOutput := "STDOUT\nSTDERR\n"

	if err := validateCheck(out, "test_check", expectedOutput); err != nil {
		t.Fatal(err)
	}

	prestart, err := r.PreStart(context.TODO(), false)
	if err != nil {
		t.Fatal(err)
	}

	if err := validateCheck(prestart, "check1", expectedOutput); err != nil {
		t.Fatal(err)
	}

	poststart, err := r.PostStart(context.TODO(), false)
	if err != nil {
		t.Fatal(err)
	}

	if err := validateCheck(poststart, "check2", expectedOutput); err != nil {
		t.Fatal(err)
	}
}

func validateCheck(cr *CombinedResponse, name, output string) error {
	if cr.Status() != 0 {
		return errors.Errorf("expect exit code 0. Got %d", cr.Status())
	}

	check, ok := cr.checks[name]
	if !ok {
		return errors.Errorf("expect check %s", name)
	}

	if check.output != output {
		return errors.Errorf("expect %s. Got %s", output, check.output)
	}
	return nil
}

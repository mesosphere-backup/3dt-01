package runner

import (
	"testing"
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

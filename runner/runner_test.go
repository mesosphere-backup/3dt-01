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

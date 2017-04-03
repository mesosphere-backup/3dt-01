package config

import (
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

// Test invalid config
func TestIncorrectField(t *testing.T) {
	userConfig := `
	{
	  "incorrect_field": true
	}
	`
	documentLoader := gojsonschema.NewStringLoader(userConfig)
	if err := validate(documentLoader); err == nil {
		t.Error("Test must fail, but it didn't")
	}
}

// Test valid config
func TestValidateSuccess(t *testing.T) {
	userConfig := `
	{
	  "port": 1024,
	  "pull": true,
	  "pull-interval": 10,
	  "role": "master"
	}
	`
	documentLoader := gojsonschema.NewStringLoader(userConfig)
	if err := validate(documentLoader); err != nil {
		t.Error(err)
	}
}

// Test less then allowed value passed
func TestPortMinimum(t *testing.T) {
	userConfig := `
	{
	  "port": 100
	}
	`
	documentLoader := gojsonschema.NewStringLoader(userConfig)
	if err := validate(documentLoader); err == nil {
		t.Error("Test must fail, but it didn't")
	}
}

// Test more then allowed value passed
func TestPortMaximum(t *testing.T) {
	userConfig := `
	{
	  "port": 65536
	}
	`
	documentLoader := gojsonschema.NewStringLoader(userConfig)
	if err := validate(documentLoader); err == nil {
		t.Error("Test must fail, but it didn't")
	}
}

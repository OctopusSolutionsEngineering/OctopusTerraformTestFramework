package test

import (
	"testing"
)

func TestCustomEnvironmentVariablesExtendInputValues(t *testing.T) {

	sut := OctopusContainerTest{}
	sut.CustomEnvironment = nil

	containerEnv := make(map[string]string)
	containerEnv["1"] = "first"
	containerEnv["2"] = "second"

	fullEnv := sut.AddCustomEnvironment(containerEnv)

	if fullEnv["1"] != "first" || fullEnv["2"] != "second" {
		t.Error("The original environment was illegally modified")
	}
}

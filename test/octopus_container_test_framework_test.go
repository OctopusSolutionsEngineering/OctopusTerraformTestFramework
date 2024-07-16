package test

import (
	"testing"
)

func TestCustomEnvironmentVariablesCanBeNil(t *testing.T) {

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

func TestCustomEnvironmentVariablesCannotOverrideImplicitValues(t *testing.T) {
	sut := OctopusContainerTest{}
	sut.CustomEnvironment = make(map[string]string)
	sut.CustomEnvironment["ACCEPT_EULA"] = "N"

	containerEnv := make(map[string]string)
	containerEnv["ACCEPT_EULA"] = "Y"

	fullEnv := sut.AddCustomEnvironment(containerEnv)

	if fullEnv["ACCEPT_EULA"] != "Y" {
		t.Error("The original environment was illegally modified")
	}
}

func TestContainerWithNoSpecifiedVersionWillUseLatest(t *testing.T) {
	sut := OctopusContainerTest{}

	version := sut.getOctopusVersion()

	if version != "latest" {
		t.Errorf("The OctopusServer version is %v", version)
	}
}

func TestContainerVersionCanBeOverridenAtConstruction(t *testing.T) {
	sut := OctopusContainerTest{}
	sut.OctopusVersion = "ArbitraryVersion"

	version := sut.getOctopusVersion()

	if version != "ArbitraryVersion" {
		t.Errorf("The OctopusServer version is %v", version)
	}
}

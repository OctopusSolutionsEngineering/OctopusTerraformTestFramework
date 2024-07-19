package test

import (
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/client"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/environments"
	"github.com/OctopusSolutionsEngineering/OctopusTerraformTestFramework/octoclient"
	"path/filepath"
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

func TestCustomEnvironmentVariablesAreAddedToEnvironment(t *testing.T) {

	sut := OctopusContainerTest{}
	sut.CustomEnvironment = make(map[string]string)

	containerEnv := make(map[string]string)
	containerEnv["1"] = "first"
	containerEnv["2"] = "second"

	fullEnv := sut.AddCustomEnvironment(containerEnv)

	if fullEnv["1"] != "first" || fullEnv["2"] != "second" {
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

// TestCreateEnvironments is an example of the kind of tests that can be written using the OctopusContainerTest framework.
func TestCreateEnvironments(t *testing.T) {
	testFramework := OctopusContainerTest{}
	testFramework.ArrangeTest(t, func(t *testing.T, container *OctopusContainer, client *client.Client) error {
		// Act
		newSpaceId, err := testFramework.Act(
			t,
			container,
			filepath.Join("..", "terraform"), "2-simpleexample", []string{})

		if err != nil {
			return err
		}

		newSpaceClient, err := octoclient.CreateClient(container.URI, newSpaceId, ApiKey)

		if err != nil {
			return err
		}

		testEnvironments, err := environments.GetAll(newSpaceClient, newSpaceId)

		if err != nil {
			return err
		}

		if len(testEnvironments) != 3 {
			t.Fatalf("Expected 3 environments, got %d", len(testEnvironments))
		}

		return nil
	})
}

package test

import (
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/client"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/environments"
	"github.com/OctopusSolutionsEngineering/OctopusTerraformTestFramework/octoclient"
	"path/filepath"
	"testing"
)

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

This module is a reusable test framework to create an Octopus instance connected to MSSQL and populated with
Terraform modules.

An example of a test looks like this:

```go
package main

import (
	"fmt"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/client"
	"github.com/mcasperson/OctopusTerraformTestFramework/test"
	"path/filepath"
	"testing"
)

func TestCreateSpaceAndUseIt(t *testing.T) {
	testFramework := test.OctopusContainerTest{}
    testFramework.ArrangeTest(t, func(t *testing.T, container *test.OctopusContainer, client *client.Client) error {
        _, err := testFramework.Act(t, container, filepath.Join("terraform", "2-usenewspace"), []string{})
        return err
    })
	
}
```
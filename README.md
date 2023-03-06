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

## Environment variables

* `OCTOTESTWAITFORAPI` - set to `false` to remove the check of the API between creating a space and populating it. The default is to run these checks.
* `OCTOTESTVERSION` - set to the tag of the `octopusdeploy/octopusdeploy` Docker image to use in the tests. The default is `latest`.
* `OCTOTESTRETRYCOUNT` - set to the number of retries to use for any individual test. Defaults to 3.
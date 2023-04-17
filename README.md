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
        _, err := testFramework.Act(t, container, "terraform", "2-usenewspace", []string{})
        return err
    })
	
}
```

You must have a directory called `1-singlespace` as a sibling to the directory called in the `Act` method. This directory
must contain a Terraform module that creates a new Octopus space and returns the new space ID as teh output variable `octopus_space_id`.
For example:

```
test
  - terraform
    - 1-singlespace
    - 2-usenewspace
```

An example of this directory has been provided at [1-singlespace](terraform%2F1-singlespace).

## Environment variables

* `OCTOTESTWAITFORAPI` - set to `false` to remove the check of the API between creating a space and populating it. The default is to run these checks.
* `OCTOTESTVERSION` - set to the tag of the `octopusdeploy/octopusdeploy` Docker image to use in the tests. The default is `latest`.
* `OCTOTESTRETRYCOUNT` - set to the number of retries to use for any individual test. Defaults to 3.
* `OCTOTESTDUMPSTATE` - set to `true` to dump the Terraform state if a request for an output variable fails. Defaults to `false`.
* `OCTOTESTDEFAULTSPACEID` - Terraform seems to have a bug where the state file is not written correctly. If this happens, the ID of the newly created space can not be read. Setting this env var allows you to recover from this error by setting the default value of the new space (usually `Spaces-2`).
* `OCTOTESTSKIPINIT` - set to true to skip `terraform init`. Skipping the init phase is useful when you define a provider override in the `~/.terraformrc` file.
* `OCTODISABLEOCTOCONTAINERLOGGING` - set to true to skip logging output from the Octopus container.
* `LICENSE` - Set to the base 64 encoded version of an Octopus XML license. See `Test Octopus License` under `Shared-Sales` in Lasptpass for a value.

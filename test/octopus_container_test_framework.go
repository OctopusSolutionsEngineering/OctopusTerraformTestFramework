package test

import (
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/client"
	"github.com/OctopusSolutionsEngineering/OctopusTerraformTestFramework/octoclient"
	lintwait "github.com/OctopusSolutionsEngineering/OctopusTerraformTestFramework/wait"
	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
	cp "github.com/otiai10/copy"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
	This file contains a bunch of functions to support integration tests with a live Octopus instance hosted
	in a Docker container and managed by test containers.
*/

const ApiKey = "API-ABCDEFGHIJKLMNOPQURTUVWXYZ12345"

type InitializationSettings struct {
	InputVars        []string
	SpaceIdOutputVar string
}

type OctopusContainer struct {
	testcontainers.Container
	URI string
}

type MysqlContainer struct {
	testcontainers.Container
	port string
	ip   string
}

type TestLogConsumer struct {
}

func (g *TestLogConsumer) Accept(l testcontainers.Log) {
	fmt.Println(string(l.Content))
}

var globalMutex = sync.Mutex{}

type OctopusContainerTest struct {
	CustomEnvironment map[string]string
}

func (o *OctopusContainerTest) enableContainerLogging(container testcontainers.Container, ctx context.Context) error {
	// Display the container logs
	err := container.StartLogProducer(ctx)
	if err != nil {
		return err
	}
	g := TestLogConsumer{}
	container.FollowOutput(&g)
	return nil
}

// getProvider returns the test containers provider
func (o *OctopusContainerTest) getProvider() testcontainers.ProviderType {
	if strings.Contains(os.Getenv("DOCKER_HOST"), "podman") {
		return testcontainers.ProviderPodman
	}

	return testcontainers.ProviderDocker
}

// setupNetwork creates an internal network for Octopus and MS SQL
func (o *OctopusContainerTest) setupNetwork(ctx context.Context) (testcontainers.Network, string, error) {
	name := "octotera" + uuid.New().String()

	network, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name: name,
			// Option CheckDuplicate is there to provide a best effort checking of any networks
			// which has the same name but it is not guaranteed to catch all name collisions.
			CheckDuplicate: false,
		},
		ProviderType: o.getProvider(),
	})

	return network, name, err
}

// setupDatabase creates a MSSQL container
func (o *OctopusContainerTest) setupDatabase(ctx context.Context, network string) (*MysqlContainer, error) {
	req := testcontainers.ContainerRequest{
		Name:         "mssql-" + uuid.New().String(),
		Image:        "mcr.microsoft.com/mssql/server" + o.getMSSQLTaggedVersion(),
		ExposedPorts: []string{"1433/tcp"},
		Env: map[string]string{
			"ACCEPT_EULA": "Y",
			"SA_PASSWORD": "Password01!",
		},
		WaitingFor: wait.ForExec([]string{"/opt/mssql-tools18/bin/sqlcmd", "-C", "-U", "sa", "-P", "Password01!", "-Q", "select 1"}).WithStartupTimeout(60 * time.Second).WithExitCodeMatcher(
			func(exitCode int) bool {
				return exitCode == 0
			}),
		Networks: []string{
			network,
		},
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Reuse:            false,
	})
	if err != nil {
		return nil, err
	}

	ip, err := container.Host(ctx)
	if err != nil {
		return nil, err
	}

	mappedPort, err := container.MappedPort(ctx, "1433")
	if err != nil {
		return nil, err
	}

	return &MysqlContainer{
		Container: container,
		ip:        ip,
		port:      mappedPort.Port(),
	}, nil
}

func (o *OctopusContainerTest) getMSSQLTaggedVersion() string {
	overrideMSSQLOctoTag := os.Getenv("OCTO_MSSQLTAG")
	if overrideMSSQLOctoTag != "" {
		return ":" + overrideMSSQLOctoTag
	}

	return ""
}

func (o *OctopusContainerTest) getOctopusImageUrl() string {
	overrideImageUrl := os.Getenv("OCTOTESTIMAGEURL")
	if overrideImageUrl != "" {
		return overrideImageUrl
	}

	return "octopusdeploy/octopusdeploy"
}

func (o *OctopusContainerTest) getOctopusVersion() string {
	overrideOctoTag := os.Getenv("OCTOTESTVERSION")
	if overrideOctoTag != "" {
		return overrideOctoTag
	}

	return "latest"
}

func (o *OctopusContainerTest) getRetryCount() uint {
	count, err := strconv.Atoi(os.Getenv("OCTOTESTRETRYCOUNT"))
	if err == nil && count > 0 {
		return uint(count)
	}

	return 3
}

// setupOctopus creates an Octopus container
func (o *OctopusContainerTest) setupOctopus(ctx context.Context, connString string, network string) (*OctopusContainer, error) {
	if os.Getenv("LICENSE") == "" {
		return nil, errors.New("the LICENSE environment variable must be set to a base 64 encoded Octopus license key")
	}

	if _, err := b64.StdEncoding.DecodeString(os.Getenv("LICENSE")); err != nil {
		return nil, errors.New("the LICENSE environment variable must be set to a base 64 encoded Octopus license key")
	}

	disableDind := os.Getenv("OCTODISABLEDIND")
	if disableDind == "" {
		disableDind = "Y"
	}

	req := testcontainers.ContainerRequest{
		Name:         "octopus-" + uuid.New().String(),
		Image:        o.getOctopusImageUrl() + ":" + o.getOctopusVersion(),
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"ACCEPT_EULA":          "Y",
			"DB_CONNECTION_STRING": connString,
			// CONNSTRING, LICENSE_BASE64, and CREATE_DB are used by the octopusdeploy/linux image
			"CONNSTRING":                    connString,
			"CREATE_DB":                     "Y",
			"ADMIN_API_KEY":                 getApiKey(),
			"DISABLE_DIND":                  disableDind,
			"ADMIN_USERNAME":                "admin",
			"ADMIN_PASSWORD":                "Password01!",
			"OCTOPUS_SERVER_BASE64_LICENSE": os.Getenv("LICENSE"),
			"LICENSE_BASE64":                os.Getenv("LICENSE"),
			"ENABLE_USAGE":                  "N",
		},
		Privileged: disableDind != "Y",
		WaitingFor: wait.ForLog("Listening for HTTP requests on").WithStartupTimeout(30 * time.Minute),
		Networks: []string{
			network,
		},
	}

	req.Env = o.AddCustomEnvironment(req.Env)

	log.Println("Creating Octopus container")
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Reuse:            false,
	})
	if err != nil {
		return nil, err
	}
	log.Println("Finished creating Octopus container")

	// Display the container logs
	if os.Getenv("OCTODISABLEOCTOCONTAINERLOGGING") != "true" {
		o.enableContainerLogging(container, ctx)
	}

	ip, err := container.Host(ctx)
	if err != nil {
		return nil, err
	}

	mappedPort, err := container.MappedPort(ctx, "8080")
	if err != nil {
		return nil, err
	}

	uri := fmt.Sprintf("http://%s:%s", ip, mappedPort.Port())

	return &OctopusContainer{Container: container, URI: uri}, nil
}

func getApiKey() string {
	apiKey := os.Getenv("OCTOTESTAPIKEY")
	if apiKey == "" {
		return ApiKey
	}
	return apiKey
}

// Pass through feature flags in the current environment to the environment used
// to launch the OctopusDeploy container
func (o *OctopusContainerTest) AddCustomEnvironment(input map[string]string) map[string]string {
	result := make(map[string]string)

	for k, v := range input {
		result[k] = v
	}

	for k, v := range o.CustomEnvironment {
		//checking against the input, as `result` is growing on each iteration
		value, exists := input[k]
		if exists {
			log.Println(k + " already exists in OctopusServer's environment as '" + value + "', and will not be replaced")
		} else {
			result[k] = v
		}
	}

	return result
}

// createDockerInfrastructure attempts to create the complete Docker stack containing a
// network, MSSQL container, and Octopus container. The return values include as much of
// the partial stack as possible in the case of an error.
func (o *OctopusContainerTest) createDockerInfrastructure(t *testing.T, ctx context.Context) (testcontainers.Network, *OctopusContainer, *MysqlContainer, error) {

	network, networkName, err := o.setupNetwork(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	sqlServer, err := o.setupDatabase(ctx, networkName)
	if err != nil {
		return network, nil, sqlServer, err
	}

	sqlIp, err := sqlServer.Container.ContainerIP(ctx)
	if err != nil {
		return network, nil, sqlServer, err
	}

	sqlName, err := sqlServer.Container.Name(ctx)
	if err != nil {
		return network, nil, sqlServer, err
	}

	t.Log("SQL Server IP: " + sqlIp)
	t.Log("SQL Server Container Name: " + sqlName)

	octopusContainer, err := o.setupOctopus(ctx, "Server="+sqlIp+",1433;Database=OctopusDeploy;User=sa;Password=Password01!", networkName)
	if err != nil {
		return network, octopusContainer, sqlServer, err
	}

	octoIp, err := octopusContainer.Container.ContainerIP(ctx)
	if err != nil {
		return network, octopusContainer, sqlServer, err
	}

	octoName, err := octopusContainer.Container.Name(ctx)
	if err != nil {
		return network, octopusContainer, sqlServer, err
	}

	t.Log("Octopus IP: " + octoIp)
	t.Log("Octopus Container Name: " + octoName)

	return network, octopusContainer, sqlServer, nil
}

// ArrangeContainer is wrapper that initialises Octopus, and returns the container for future test runs
func (o *OctopusContainerTest) ArrangeContainer() (*OctopusContainer, *client.Client, *MysqlContainer, testcontainers.Network, error) {
	var octopusContainer *OctopusContainer
	var octoClient *client.Client
	var network testcontainers.Network
	var networkName string
	var sqlServer *MysqlContainer

	err := retry.Do(
		func() error {
			ctx := context.Background()

			var err error
			log.Print("Setting up network")
			network, networkName, err = o.setupNetwork(ctx)
			if err != nil {
				log.Print("Failed to setup network container")
				return err
			}

			sqlServer, err = o.setupDatabase(ctx, networkName)
			if err != nil {
				log.Print("Failed to setup mssql database container")
				return err
			}

			sqlIp, err := sqlServer.Container.ContainerIP(ctx)
			if err != nil {
				log.Print("Failed to setup container IP container")
				return err
			}

			sqlName, err := sqlServer.Container.Name(ctx)
			if err != nil {
				log.Print("Failed to get sql container name")
				return err
			}

			log.Println("SQL Server IP: " + sqlIp)
			log.Println("SQL Server Container Name: " + sqlName)

			octopusContainer, err = o.setupOctopus(ctx, "Server="+sqlIp+",1433;Database=OctopusDeploy;User=sa;Password=Password01!", networkName)
			if err != nil {
				log.Print("Failed to setup octopus container")
				return err
			}

			octoIp, err := octopusContainer.Container.ContainerIP(ctx)
			if err != nil {
				log.Print("Failed to get octopus container IP")
				return err
			}

			octoName, err := octopusContainer.Container.Name(ctx)
			if err != nil {
				log.Print("Failed to get octopus container Name")
				return err
			}

			log.Println("Octopus IP: " + octoIp)
			log.Println("Octopus Container Name: " + octoName)

			// give the server 5 minutes to start up
			err = lintwait.WaitForResource(func() error {
				resp, err := http.Get(octopusContainer.URI + "/api")
				if err != nil || resp.StatusCode != http.StatusOK {
					return errors.New("the api endpoint was not available")
				}
				return nil
			}, 5*time.Minute)

			if err != nil {
				return err
			}

			octoClient, err = octoclient.CreateClient(octopusContainer.URI, "", getApiKey())
			if err != nil {
				return err
			}

			return nil
		},
		retry.Attempts(o.getRetryCount()),
		retry.Delay(30*time.Second),
	)

	if err != nil {
		log.Println(err.Error())
		return nil, nil, nil, nil, err
	}

	return octopusContainer, octoClient, sqlServer, network, nil
}

// CleanUp stops the containers after the test is complete
func (o *OctopusContainerTest) CleanUp(ctx context.Context, octoContainer *OctopusContainer, sqlServer *MysqlContainer, network testcontainers.Network) error {
	// Stop the containers
	stopTime := 1 * time.Minute
	if octoStopErr := octoContainer.Stop(ctx, &stopTime); octoStopErr != nil {
		log.Println("Failed to stop the Octopus container:", octoStopErr)
	}

	if sqlStopErr := sqlServer.Container.Stop(ctx, &stopTime); sqlStopErr != nil {
		log.Println("Failed to stop the SQL Server container:", sqlStopErr)
	}

	// Terminate the containers
	if octoTerminateErr := octoContainer.Terminate(ctx); octoTerminateErr != nil {
		log.Printf("Failed to terminate the Octopus container: %v", octoTerminateErr)
	}

	if sqlTerminateErr := sqlServer.Container.Terminate(ctx); sqlTerminateErr != nil {
		log.Printf("Failed to terminate the SQL Server container: %v", sqlTerminateErr)
	}

	if networkErr := network.Remove(ctx); networkErr != nil {
		log.Printf("Failed to remove network: %v", networkErr)
	}

	return nil
}

// ArrangeTest is wrapper that initialises Octopus, runs a test, and cleans up the containers
func (o *OctopusContainerTest) ArrangeTest(t *testing.T, testFunc func(t *testing.T, container *OctopusContainer, client *client.Client) error) {
	err := retry.Do(
		func() error {

			if testing.Short() {
				t.Skip("skipping integration test")
			}

			ctx := context.Background()

			// I don't think test containers are thread safe - parallel tests
			// frequently show that multiple tests access the same containers.
			// So only one thread can create a stack at a time
			globalMutex.Lock()
			network, octopusContainer, sqlServer, err := o.createDockerInfrastructure(t, ctx)
			globalMutex.Unlock()

			// Attempt to clean up whatever resources were created.
			// Don't return errors for the cleanup, just report them
			defer func() {
				globalMutex.Lock()
				defer globalMutex.Unlock()

				stopTime := 1 * time.Minute

				if octopusContainer != nil {
					// This fixes the "can not get logs from container which is dead or marked for removal" error
					// See https://github.com/testcontainers/testcontainers-go/issues/606
					if os.Getenv("OCTODISABLEOCTOCONTAINERLOGGING") != "true" {
						stopProducerErr := octopusContainer.StopLogProducer()

						// try to continue on if there was an error stopping the producer
						if stopProducerErr != nil {
							t.Log(stopProducerErr)
						}
					}

					// Stop the containers
					octoStopErr := octopusContainer.Stop(ctx, &stopTime)

					if octoStopErr != nil {
						t.Log("Failed to stop the Octopus container")
					}

					octoTerminateErr := octopusContainer.Terminate(ctx)

					if octoTerminateErr != nil {
						t.Log("Failed to terminate the Octopus container")
					}
				}

				if sqlServer != nil {
					sqlStopErr := sqlServer.Stop(ctx, &stopTime)

					if sqlStopErr != nil {
						t.Log("Failed to stop the MSSQL container")
					}

					sqlTerminateErr := sqlServer.Terminate(ctx)

					if sqlTerminateErr != nil {
						t.Log("Failed to terminate the MSSQL container")
					}
				}

				// Terminate the containers
				if network != nil {
					networkErr := network.Remove(ctx)

					if networkErr != nil {
						t.Logf("failed to remove network: %v", networkErr)
					}
				}
			}()

			// In the event of a failed stack creation, use the defer function above
			// to clean up and then return the error
			if err != nil {
				return err
			}

			// give the server 5 minutes to start up
			err = lintwait.WaitForResource(func() error {
				resp, err := http.Get(octopusContainer.URI + "/api")
				if err != nil || resp.StatusCode != http.StatusOK {
					return errors.New("the api endpoint was not available")
				}
				return nil
			}, 5*time.Minute)

			if err != nil {
				return err
			}

			client, err := octoclient.CreateClient(octopusContainer.URI, "", getApiKey())
			if err != nil {
				return err
			}

			err = testFunc(t, octopusContainer, client)

			if err != nil {
				t.Log(err.Error())
			}

			return err
		},
		retry.Attempts(o.getRetryCount()),
		retry.Delay(30*time.Second),
	)

	if err != nil {
		t.Fatalf(err.Error())
	}
}

// cleanTerraformModule removes state and lock files to ensure we get a clean run each time
func (o *OctopusContainerTest) cleanTerraformModule(terraformProjectDir string) error {
	err := retry.Do(func() error {
		err := o.deleteIfExists(filepath.Join(terraformProjectDir, ".terraform.lock.hcl"))
		if err != nil {
			return err
		}

		err = o.deleteIfExists(filepath.Join(terraformProjectDir, "terraform.tfstate"))
		if err != nil {
			return err
		}

		err = o.deleteIfExists(filepath.Join(terraformProjectDir, ".terraform.tfstate.lock.info"))
		if err != nil {
			return err
		}

		return nil
	}, retry.Attempts(3))

	return err
}

func (o *OctopusContainerTest) deleteIfExists(file string) error {
	err := os.Remove(file)

	if err != nil && os.IsNotExist(err) {
		return nil
	}

	return err
}

// TerraformInit runs "terraform init"
func (o *OctopusContainerTest) TerraformInit(t *testing.T, terraformProjectDir string) error {
	args := []string{"init", "-no-color"}
	cmnd := exec.Command("terraform", args...)
	cmnd.Dir = terraformProjectDir
	out, err := cmnd.Output()

	t.Log(string(out))

	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			t.Log("terraform init error: " + string(exitError.Stderr))
		} else {
			t.Log(err.Error())
		}

		return err
	}

	return nil
}

// TerraformApply runs "terraform apply"
func (o *OctopusContainerTest) TerraformApply(t *testing.T, terraformProjectDir string, server string, spaceId string, vars []string) error {
	newArgs := append([]string{
		"apply",
		"-auto-approve",
		"-no-color",
		"-var=octopus_server=" + server,
		"-var=octopus_apikey=" + getApiKey(),
		"-var=octopus_space_id=" + spaceId,
	}, vars...)

	cmnd := exec.Command("terraform", newArgs...)
	cmnd.Dir = terraformProjectDir
	out, err := cmnd.Output()

	t.Log(string(out))

	if err != nil {
		t.Log("server: " + server)
		t.Log("spaceId: " + spaceId)

		exitError, ok := err.(*exec.ExitError)
		if ok {
			t.Log("terraform apply error")
			t.Log(string(exitError.Stderr))
		} else {
			t.Log(err)
		}
		return err
	}

	return nil
}

// waitForSpace attempts to ensure the API and space is available before continuing
func (o *OctopusContainerTest) waitForSpace(t *testing.T, server string, spaceId string) {
	if os.Getenv("OCTOTESTWAITFORAPI") == "false" {
		return
	}

	// Error like:
	// Error: Octopus API error: Resource is not found or it doesn't exist in the current space context. Please contact your administrator for more information. []
	// are sometimes proceeded with:
	// "HTTP" "GET" to "localhost:32805""/api" "completed" with 503 in 00:00:00.0170358 (17ms) by "<anonymous>"
	// So wait until we get a valid response from the API endpoint before applying terraform
	err := lintwait.WaitForResource(func() error {
		response, err := http.Get(server + "/api")
		if err != nil {
			return err
		}
		if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
			return errors.New("non 2xx status code returned")
		}
		return nil
	}, 5*time.Minute)

	if err != nil {
		t.Log("Failed to contact Octopus API on " + server + "/api")
	}

	// Also wait for the space to be available
	err = lintwait.WaitForResource(func() error {
		response, err := http.Get(server + "/api/" + spaceId)
		if err != nil {
			return err
		}
		if !(response.StatusCode >= 200 && response.StatusCode <= 299) {
			return errors.New("non 2xx status code returned")
		}
		return nil
	}, 5*time.Minute)

	if err != nil {
		t.Log("Failed to contact Octopus API on " + server + "/api/" + spaceId)
	}
}

// TerraformInitAndApply calls terraform init and apply on the supplied directory.
func (o *OctopusContainerTest) TerraformInitAndApply(t *testing.T, container *OctopusContainer, terraformProjectDir string, spaceId string, vars []string) error {
	o.cleanTerraformModule(terraformProjectDir)

	if strings.ToLower(os.Getenv("OCTOTESTSKIPINIT")) != "true" {
		err := o.TerraformInit(t, terraformProjectDir)

		if err != nil {
			return err
		}
	}

	return o.TerraformApply(t, terraformProjectDir, container.URI, spaceId, vars)
}

// InitialiseOctopus uses Terraform to populate the test Octopus instance, making sure to clean up
// any files generated during previous Terraform executions to avoid conflicts and locking issues.
func (o *OctopusContainerTest) InitialiseOctopus(
	t *testing.T,
	container *OctopusContainer,
	terraformInitModuleDir string,
	prepopulateModuleDir string,
	terraformModuleDir string,
	spaceName string,
	initialiseVars []string,
	prepopulateVars []string,
	populateVars []string) error {

	path, err := os.Getwd()
	if err != nil {
		return err
	}
	t.Log("Working dir: " + path)

	// This test creates a new space and then populates the space.
	terraformProjectDirs := orderedmap.New[string, InitializationSettings]()
	terraformProjectDirs.Set(terraformInitModuleDir, InitializationSettings{
		InputVars:        append(initialiseVars, "-var=octopus_space_name="+spaceName, "-var=octopus_space_description="+t.Name()),
		SpaceIdOutputVar: "octopus_space_id",
	})
	if prepopulateModuleDir != "" {
		terraformProjectDirs.Set(prepopulateModuleDir, InitializationSettings{
			InputVars:        prepopulateVars,
			SpaceIdOutputVar: "",
		})
	}
	terraformProjectDirs.Set(terraformModuleDir, InitializationSettings{
		InputVars:        populateVars,
		SpaceIdOutputVar: "",
	})

	// First loop initialises the new space, second populates the space
	spaceId := "Spaces-1"
	for pair := terraformProjectDirs.Oldest(); pair != nil; pair = pair.Next() {
		terraformProjectDir := pair.Key
		settings := pair.Value

		o.cleanTerraformModule(terraformProjectDir)

		if strings.ToLower(os.Getenv("OCTOTESTSKIPINIT")) != "true" {
			err := o.TerraformInit(t, terraformProjectDir)

			if err != nil {
				return err
			}
		}

		o.waitForSpace(t, container.URI, spaceId)

		err = o.TerraformApply(t, terraformProjectDir, container.URI, spaceId, settings.InputVars)

		if err != nil {
			return err
		}

		// get the ID of any new space created, which will be used in the subsequent Terraform executions
		if settings.SpaceIdOutputVar != "" {
			spaceId, err = o.GetOutputVariable(t, terraformProjectDir, settings.SpaceIdOutputVar)
			if err != nil || len(strings.TrimSpace(spaceId)) == 0 {
				// I've seen number of tests fail because the state file is blank and there is no output to read.
				// We offer a workaround for this by setting the default space ID, which is usually Spaces-2
				if os.Getenv("OCTOTESTDEFAULTSPACEID") != "" {
					spaceId = os.Getenv("OCTOTESTDEFAULTSPACEID")
				} else {
					return err
				}
			}
		}
	}

	return nil
}

// GetOutputVariable reads a Terraform output variable
func (o *OctopusContainerTest) GetOutputVariable(t *testing.T, terraformDir string, outputVar string) (string, error) {

	// Note that you "terraform output -raw" can still get a 0 exit code if there was an error:
	// https://github.com/hashicorp/terraform/issues/32384
	// So we must get the JSON.
	cmnd := exec.Command(
		"terraform",
		"output",
		"-json",
		outputVar)
	cmnd.Dir = terraformDir
	out, err := cmnd.Output()

	if err != nil {
		if os.Getenv("OCTOTESTDUMPSTATE") == "true" {
			o.ShowState(t, terraformDir)
		}
		exitError, ok := err.(*exec.ExitError)
		if ok {
			t.Log("terraform output error: " + string(exitError.Stderr))
		} else {
			t.Log(err)
		}
		return "", err
	}

	data := ""
	err = json.Unmarshal(out, &data)

	if err != nil {
		return "", err
	}

	return data, nil
}

// ShowState reads the terraform state
func (o *OctopusContainerTest) ShowState(t *testing.T, terraformDir string) error {
	cmnd := exec.Command(
		"terraform",
		"show",
		"-json")
	cmnd.Dir = terraformDir
	out, err := cmnd.Output()

	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			t.Log("terraform show return code: " + string(exitError.Stderr))
		} else {
			t.Log(err)
		}
		return err
	}

	t.Log(string(out))

	if err != nil {
		return err
	}

	return nil
}

// Act initialises Octopus and MSSQL
func (o *OctopusContainerTest) Act(t *testing.T, container *OctopusContainer, terraformBaseDir string, terraformModuleDir string, populateVars []string) (string, error) {
	spaceName := strings.ReplaceAll(fmt.Sprint(uuid.New()), "-", "")[:20]
	t.Log("POPULATING TEST SPACE " + spaceName)

	spacePopulateDir := filepath.Join(terraformBaseDir, "1-singlespace")
	dir, err := o.copyDir(spacePopulateDir)

	if err != nil {
		return "", err
	}

	defer func() {
		err := os.RemoveAll(dir)
		if err != nil {
			t.Fatalf(err.Error())
		}
	}()

	err = o.InitialiseOctopus(t, container, dir, "", filepath.Join(terraformBaseDir, terraformModuleDir), spaceName, []string{}, []string{}, populateVars)

	if err != nil {
		return "", err
	}

	spaceId, err := o.GetOutputVariable(t, dir, "octopus_space_id")

	if err != nil || len(strings.TrimSpace(spaceId)) == 0 {
		// I've seen number of tests fail because the state file is blank and there is no output to read.
		// We offer a workaround for this by setting the default space ID, which is usually Spaces-2
		if os.Getenv("OCTOTESTDEFAULTSPACEID") != "" {
			spaceId = os.Getenv("OCTOTESTDEFAULTSPACEID")
			return spaceId, nil
		} else {
			return "", err
		}
	}

	return spaceId, err
}

// ActWithCustomSpace initialises Octopus and MSSQL with a custom directory holding the module to create the initial space
func (o *OctopusContainerTest) ActWithCustomSpace(t *testing.T, container *OctopusContainer, initialiseModuleDir string, terraformModuleDir string, initialiseVars []string, populateVars []string) (string, error) {
	spaceName := strings.ReplaceAll(fmt.Sprint(uuid.New()), "-", "")[:20]
	t.Log("POPULATING TEST SPACE " + spaceName)

	err := o.InitialiseOctopus(t, container, initialiseModuleDir, "", terraformModuleDir, spaceName, initialiseVars, []string{}, populateVars)

	if err != nil {
		return "", err
	}

	spaceId, err := o.GetOutputVariable(t, initialiseModuleDir, "octopus_space_id")

	if err != nil || len(strings.TrimSpace(spaceId)) == 0 {
		// I've seen number of tests fail because the state file is blank and there is no output to read.
		// We offer a workaround for this by setting the default space ID, which is usually Spaces-2
		if os.Getenv("OCTOTESTDEFAULTSPACEID") != "" {
			spaceId = os.Getenv("OCTOTESTDEFAULTSPACEID")
			return spaceId, nil
		} else {
			return "", err
		}
	}

	return spaceId, err
}

// ActWithCustomPrePopulatedSpace initialises Octopus and MSSQL with a custom directory holding the module to create the initial space and a module used to prepopulate the space
func (o *OctopusContainerTest) ActWithCustomPrePopulatedSpace(t *testing.T, container *OctopusContainer, initialiseModuleDir string, prepopulateModuleDir string, terraformModuleDir string, initialiseVars []string, prePopulateVars []string, populateVars []string) (string, error) {
	spaceName := strings.ReplaceAll(fmt.Sprint(uuid.New()), "-", "")[:20]
	t.Log("POPULATING TEST SPACE " + spaceName)

	err := o.InitialiseOctopus(t, container, initialiseModuleDir, prepopulateModuleDir, terraformModuleDir, spaceName, initialiseVars, prePopulateVars, populateVars)

	if err != nil {
		return "", err
	}

	spaceId, err := o.GetOutputVariable(t, initialiseModuleDir, "octopus_space_id")

	if err != nil || len(strings.TrimSpace(spaceId)) == 0 {
		// I've seen number of tests fail because the state file is blank and there is no output to read.
		// We offer a workaround for this by setting the default space ID, which is usually Spaces-2
		if os.Getenv("OCTOTESTDEFAULTSPACEID") != "" {
			spaceId = os.Getenv("OCTOTESTDEFAULTSPACEID")
			return spaceId, nil
		} else {
			return "", err
		}
	}

	return spaceId, err
}

func (o *OctopusContainerTest) copyDir(source string) (string, error) {
	dest, err := os.MkdirTemp("", "octoterra")
	if err != nil {
		return "", err
	}
	err = cp.Copy(source, dest)

	return dest, err
}

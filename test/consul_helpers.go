package test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/gcp"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/retry"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/gruntwork-io/terratest/modules/test-structure"
	"github.com/hashicorp/consul/api"
)

var (
	// RepoRoot represents the root of the project.
	RepoRoot = "../"

	ConsulClusterExampleVarProject = "gcp_project"
	ConsulClusterExampleVarRegion  = "gcp_region"
	ConsulClusterExampleVarZone    = "gcp_zone"

	ConsulClusterExampleVarServerClusterName = "consul_server_cluster_name"
	ConsulClusterExampleVarClientClusterName = "consul_client_cluster_name"

	ConsulClusterExampleVarServerClusterTagName = "consul_server_cluster_tag_name"
	ConsulClusterExampleVarClientClusterTagName = "consul_client_cluster_tag_name"

	ConsulClusterExampleVarServerSourceImage = "consul_server_source_image"
	ConsulClusterExampleVarClientSourceImage = "consul_client_source_image"

	ConsulClusterExampleVarServerClusterSize = "consul_server_cluster_size"
	ConsulClusterExampleVarClientClusterSize = "consul_client_cluster_size"

	ConsulClusterExampleDefaultNumServers = 3
	ConsulClusterExampleDefaultNumClients = 4

	ConsulClusterExampleOutputServerInstanceGroupName = "instance_group_name"
	ConsulClusterExampleOutputClientInstanceGroupName = "client_instance_group_name"

	ConsulClusterServerAllowedInboundCidrBlockHttpApi = "consul_server_allowed_inbound_cidr_blocks_http_api"
	ConsulClusterServerAllowedInboundCidrBlockDns     = "consul_server_allowed_inbound_cidr_blocks_dns"

	ConsulClusterClientAllowedInboundCidrBlockHttpApi = "consul_client_allowed_inbound_cidr_blocks_http_api"
	ConsulClusterClientAllowedInboundCidrBlockDns     = "consul_client_allowed_inbound_cidr_blocks_dns"

	// SavedGCPZone represents the key to use when saving the GCP zone.
	SavedGCPZone = "GCPZone"
)

// Test the consul-cluster example by:
//
// 1. Copying the code in this repo to a temp folder so tests on the Terraform code can run in parallel without the
//    state files overwriting each other.
// 2. Building the Image in the consul-image example with the given build name
// 3. Deploying that Image using the consul-cluster Terraform code
// 4. Checking that the Consul cluster comes up within a reasonable time period and can respond to requests
func runConsulClusterTest(t *testing.T, packerBuildName string, examplesFolder string, packerTemplatePath string) {
	exampleFolder := test_structure.CopyTerraformFolderToTemp(t, RepoRoot, examplesFolder)

	test_structure.RunTestStage(t, "setup_image", func() {
		// Get the Project Id to use
		gcpProjectID := gcp.GetGoogleProjectIDFromEnvVar()

		// Pick a random GCP zone to test in. This helps ensure your code works in all regions and zones.
		gcpZone := gcp.GetRandomZone(t, nil, nil)

		test_structure.SaveString(t, exampleFolder, SavedGCPZone, gcpZone)

		// Make sure the Packer build completes successfully
		imageID := buildImage(t, packerTemplatePath, packerBuildName, gcpProjectID, gcpZone)
		test_structure.SaveArtifactID(t, exampleFolder, imageID)
	})

	defer test_structure.RunTestStage(t, "teardown", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, exampleFolder)
		terraform.Destroy(t, terraformOptions)

		// Get the Project Id to use
		gcpProjectID := gcp.GetGoogleProjectIDFromEnvVar()

		imageID := test_structure.LoadArtifactID(t, exampleFolder)
		defer gcp.DeleteImage(t, gcpProjectID, imageID)
	})

	test_structure.RunTestStage(t, "deploy", func() {
		// GCP only supports lowercase names for some resources
		uniqueID := strings.ToLower(random.UniqueId())
		serverClusterName := fmt.Sprintf("consul-server-cluster-%s", uniqueID)
		clientClusterName := fmt.Sprintf("consul-client-cluster-%s", uniqueID)
		gcpProjectID := gcp.GetGoogleProjectIDFromEnvVar()
		gcpRegionID := gcp.GetGoogleRegionFromEnvVar()
		gcpZone := test_structure.LoadString(t, exampleFolder, SavedGCPZone)
		imageID := test_structure.LoadArtifactID(t, exampleFolder)

		terraformOptions := &terraform.Options{
			TerraformDir: exampleFolder,
			Vars: map[string]interface{}{
				ConsulClusterExampleVarProject:                    gcpProjectID,
				ConsulClusterExampleVarRegion:                     gcpRegionID,
				ConsulClusterExampleVarZone:                       gcpZone,
				ConsulClusterExampleVarServerClusterName:          serverClusterName,
				ConsulClusterExampleVarClientClusterName:          clientClusterName,
				ConsulClusterExampleVarServerClusterTagName:       serverClusterName,
				ConsulClusterExampleVarClientClusterTagName:       clientClusterName,
				ConsulClusterExampleVarServerSourceImage:          imageID,
				ConsulClusterExampleVarClientSourceImage:          imageID,
				ConsulClusterExampleVarServerClusterSize:          ConsulClusterExampleDefaultNumServers,
				ConsulClusterExampleVarClientClusterSize:          ConsulClusterExampleDefaultNumClients,
				ConsulClusterServerAllowedInboundCidrBlockHttpApi: []string{"0.0.0.0/0"},
				ConsulClusterServerAllowedInboundCidrBlockDns:     []string{"0.0.0.0/0"},
				ConsulClusterClientAllowedInboundCidrBlockHttpApi: []string{"0.0.0.0/0"},
				ConsulClusterClientAllowedInboundCidrBlockDns:     []string{"0.0.0.0/0"},
			},
		}
		test_structure.SaveTerraformOptions(t, exampleFolder, terraformOptions)

		terraform.InitAndApply(t, terraformOptions)
	})

	test_structure.RunTestStage(t, "validate", func() {
		gcpProjectID := gcp.GetGoogleProjectIDFromEnvVar()
		gcpZone := test_structure.LoadString(t, exampleFolder, SavedGCPZone)
		terraformOptions := test_structure.LoadTerraformOptions(t, exampleFolder)

		// Check the Consul servers
		checkConsulClusterIsWorking(t, ConsulClusterExampleOutputServerInstanceGroupName, terraformOptions, gcpProjectID, gcpZone)

		// Check the Consul clients
		checkConsulClusterIsWorking(t, ConsulClusterExampleOutputClientInstanceGroupName, terraformOptions, gcpProjectID, gcpZone)
	})
}

// Check that the Consul cluster comes up within a reasonable time period and can respond to requests
func checkConsulClusterIsWorking(t *testing.T, groupNameOutputVar string, terratestOptions *terraform.Options, projectID string, zone string) {
	groupName := terraform.OutputRequired(t, terratestOptions, groupNameOutputVar)

	// It can take a few minutes for the managed instance group to boot up
	maxRetries := 30
	timeBetweenRetries := 5 * time.Second

	// Check every 5 seconds until an instance has joined the managed instance group
	nodeIPAddress := retry.DoWithRetry(t, fmt.Sprintf("Waiting for instances in group %s", groupName), maxRetries, timeBetweenRetries, func() (string, error) {
		ip, err := getRandomPublicIPFromInstanceGroup(t, projectID, zone, groupName)

		if err != nil {
			return "", err
		}

		return ip, nil
	})

	testConsulCluster(t, nodeIPAddress)
}

// Use a Consul client to connect to the given node and use it to verify that:
//
// 1. The Consul cluster has deployed
// 2. The cluster has the expected number of members
// 3. The cluster has elected a leader
func testConsulCluster(t *testing.T, nodeIPAddress string) {
	consulClient := createConsulClient(t, nodeIPAddress)
	maxRetries := 60
	sleepBetweenRetries := 10 * time.Second
	expectedMembers := ConsulClusterExampleDefaultNumClients + ConsulClusterExampleDefaultNumServers

	leader := retry.DoWithRetry(t, "Check Consul members", maxRetries, sleepBetweenRetries, func() (string, error) {
		members, err := consulClient.Agent().Members(false)
		if err != nil {
			return "", err
		}

		if len(members) != expectedMembers {
			return "", fmt.Errorf("Expected the cluster to have %d members, but found %d", expectedMembers, len(members))
		}

		leader, err := consulClient.Status().Leader()
		if err != nil {
			return "", err
		}

		if leader == "" {
			return "", errors.New("Consul cluster returned an empty leader response, so a leader must not have been elected yet")
		}

		return leader, nil
	})

	logger.Logf(t, "Consul cluster is properly deployed and has elected leader %s", leader)
}

// Create a Consul client
func createConsulClient(t *testing.T, ipAddress string) *api.Client {
	config := api.DefaultConfig()
	config.Address = fmt.Sprintf("%s:8500", ipAddress)

	client, err := api.NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create Consul client due to error: %v", err)
	}

	return client
}

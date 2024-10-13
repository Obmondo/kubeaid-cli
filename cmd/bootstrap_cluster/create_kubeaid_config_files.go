package bootstrap_cluster

import (
	"embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

//go:embed templates/*
var KubeaidConfigFileTemplates embed.FS

type TemplateValues struct {
	CustomerID string

	GitUsername,
	GitPassword string

	config.ClusterConfig
	config.ForksConfig
	config.AWSConfig
	config.MonitoringConfig

	CAPIClusterNamespace string
}

func getTemplateValues() *TemplateValues {
	return &TemplateValues{
		CustomerID:           constants.ParsedConfig.CustomerID,
		GitUsername:          constants.ParsedConfig.Git.Username,
		GitPassword:          constants.ParsedConfig.Git.Password,
		ClusterConfig:        constants.ParsedConfig.Cluster,
		ForksConfig:          constants.ParsedConfig.Forks,
		AWSConfig:            *constants.ParsedConfig.Cloud.AWS,
		MonitoringConfig:     constants.ParsedConfig.Monitoring,
		CAPIClusterNamespace: utils.GetCapiClusterNamespace(),
	}
}

// Responsible for bootstrapping all the files for the given cluster in the kubeaid-config fork.
func createKubeaidConfigFiles(clusterDir string, gitAuthMethod transport.AuthMethod) {
	// Get templates.
	embeddedTemplateNames := getEmbeddedTemplateNames()
	templateValues := getTemplateValues()

	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(clusterDir, strings.TrimSuffix(embeddedTemplateName, ".tmpl"))
		createFileFromTemplate(destinationFilePath, embeddedTemplateName, templateValues)
	}

	// Generate Kubernetes Sealed Secret containing cloud credentials.
	generateCloudCredentialsSealedSecret(clusterDir)

	// Build KubePrometheus.
	buildKubePrometheus(clusterDir, gitAuthMethod, templateValues)
}

// Returns the list of embedded template names based on the underlying cloud provider.
func getEmbeddedTemplateNames() []string {
	// Templates common for all cloud providers.
	embeddedTemplateNames := constants.CommonEmbeddedTemplateNames

	// Add cloud provider specific templates.
	var cloudSpecificEmbeddedTemplateNames []string
	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		cloudSpecificEmbeddedTemplateNames = constants.AWSSpecificEmbeddedTemplateNames

	case (constants.ParsedConfig.Cloud.Azure != nil) || (constants.ParsedConfig.Cloud.Hetzner != nil):
		utils.Unreachable()

	default:
		utils.Unreachable()
	}
	embeddedTemplateNames = append(embeddedTemplateNames, cloudSpecificEmbeddedTemplateNames...)

	// Add Obmondo K8s Agent related templates if 'monitoring.connectObmondo' is set to true.
	if constants.ParsedConfig.Monitoring.ConnectObmondo {
		embeddedTemplateNames = append(embeddedTemplateNames,
			"argocd-apps/templates/obmondo-k8s-agent.app.yaml.tmpl",
			"argocd-apps/obmondo-k8s-agent.values.yaml.tmpl",
		)
	}

	return embeddedTemplateNames
}

// Creates file from the given template.
func createFileFromTemplate(destinationFilePath, embeddedTemplateName string, templateValues *TemplateValues) {
	// Open the destination file.
	destinationFile, err := os.OpenFile(destinationFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed opening file at %s : %v", destinationFilePath, err)
	}
	defer destinationFile.Close()

	// Execute the corresponding template with the template values. Then write the execution result
	// to that file.
	content := utils.ParseAndExecuteTemplate(&KubeaidConfigFileTemplates, path.Join("templates/", embeddedTemplateName), templateValues)
	destinationFile.Write(content)

	// If the destination file contains a Kubernetes Secret, then generate and replace it's contents
	// with corresponding Sealed Secret.
	if strings.HasPrefix(embeddedTemplateName, "sealed-secrets") {
		utils.GenerateSealedSecret(destinationFilePath)
	}

	slog.Info("Created file in KubeAid config fork", slog.String("file-path", destinationFilePath))
}

// Creates the jsonnet vars file for the cluster. Then executes KubeAid's kube-prometheus build
// script.
func buildKubePrometheus(clusterDir string, gitAuthMethod transport.AuthMethod, templateValues *TemplateValues) {
	// Create the jsonnet vars file.
	jsonnetVarsFilePath := fmt.Sprintf("%s/%s-vars.jsonnet", clusterDir, constants.ParsedConfig.Cluster.ClusterName)
	createFileFromTemplate(jsonnetVarsFilePath, constants.TemplateNameJsonnet, templateValues)

	// Create the kube-prometheus folder for the given cluster, in the kubeaid-config fork.
	kubePrometheusDir := fmt.Sprintf("%s/kube-prometheus", clusterDir)
	if err := os.MkdirAll(kubePrometheusDir, os.ModePerm); err != nil {
		log.Fatalf("Failed creating intermediate paths for dir %s : %v", kubePrometheusDir, err)
	}

	// Clone the KubeAid fork locally.
	kubeaidForkDir := constants.TempDir + "/kubeaid"
	utils.GitCloneRepo(constants.ParsedConfig.Forks.KubeaidForkURL, kubeaidForkDir, gitAuthMethod)

	// Run the KubePrometheus build script.
	slog.Info("Running KubePrometheus build script....")
	kubePrometheusBuildScriptPath := fmt.Sprintf("%s/build/kube-prometheus/build.sh", kubeaidForkDir)
	utils.ExecuteCommandOrDie(fmt.Sprintf("%s %s", kubePrometheusBuildScriptPath, clusterDir))
}

// Generates a Kubernetes Secret named 'cloud-credentials', which will be used by Cluster API to
// communicate with the underlying cloud provider.
// It then encrypts the Kubernetes Secret with the private key of the Sealed Secrets controller
// installed in the underlying Kubernetes cluster.
func generateCloudCredentialsSealedSecret(clusterDir string) {
	capiClusterNamespace := utils.GetCapiClusterNamespace()

	destinationDir := path.Join(clusterDir, "sealed-secrets/", capiClusterNamespace)
	if err := os.MkdirAll(destinationDir, 0700); err != nil {
		log.Fatalf("Failed creating intermediate directories for %s : %v", destinationDir, err)
	}
	destinationFilePath := fmt.Sprintf("%s/cloud-credentials.secret.yaml", destinationDir)

	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		utils.ExecuteCommandOrDie(fmt.Sprintf(`
      kubectl create secret generic cloud-credentials \
        --dry-run=client \
        --namespace %s \
        --from-literal=AWS_B64ENCODED_CREDENTIALS=%s \
        -o yaml > %s
      `,
			capiClusterNamespace, os.Getenv(constants.EnvNameAWSB64EcodedCredentials), destinationFilePath),
		)

	default:
		utils.Unreachable()
	}

	utils.GenerateSealedSecret(destinationFilePath)
}

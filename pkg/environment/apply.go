package environment

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/ministryofjustice/cloud-platform-cli/pkg/github"
	"github.com/ministryofjustice/cloud-platform-cli/pkg/slack"
	"github.com/ministryofjustice/cloud-platform-cli/pkg/util"
)

// Options are used to configure plan/apply sessions.
// These options are normally passed via flags in a command line.
type Options struct {
	Namespace, KubecfgPath, ClusterCtx, ClusterDir, GithubToken string
	PRNumber                                                    int
	BuildUrl                                                    string
	AllNamespaces                                               bool
	EnableApplySkip, RedactedEnv, SkipProdDestroy               bool
	BatchApplyIndex, BatchApplySize                             int
	OnlySkipFileChanged, IsApplyPipeline                        bool
}

// RequiredEnvVars is used to store values such as TF_VAR_ , github and pingdom tokens
// which are needed to perform terraform operations for a given namespace
type RequiredEnvVars struct {
	clustername        string `required:"true" envconfig:"TF_VAR_cluster_name"`
	clusterstatebucket string `required:"true" envconfig:"TF_VAR_cluster_state_bucket"`
	kubernetescluster  string `required:"true" envconfig:"TF_VAR_kubernetes_cluster"`
	githubowner        string `required:"true" envconfig:"TF_VAR_github_owner"`
	githubtoken        string `required:"true" envconfig:"TF_VAR_github_token"`
	SlackBotToken      string `required:"false" envconfig:"SLACK_BOT_TOKEN"`
	SlackWebhookUrl    string `required:"false" envconfig:"SLACK_WEBHOOK_URL"`
	pingdomapitoken    string `required:"true" envconfig:"PINGDOM_API_TOKEN"`
}

// Apply is used to store objects in a Apply/Plan session
type Apply struct {
	Options         *Options
	RequiredEnvVars RequiredEnvVars
	Applier         Applier
	Dir             string
	GithubClient    github.GithubIface
}

func notifyUserApplyFailed(prNumberInt int, slackToken, webhookUrl, buildUrl string) {
	if prNumberInt > 0 && strings.Contains(buildUrl, "http") {
		prNumber := fmt.Sprintf("%d", prNumberInt)

		slackErr := slack.Notify(prNumber, slackToken, webhookUrl, buildUrl)

		if slackErr != nil {
			fmt.Printf("Warning: Error notifying user of build error %v\n", slackErr)
		}
	}
}

// NewApply creates a new Apply object and populates its fields with values from options(which are flags),
// instantiate Applier object which also checks and sets the Backend config variables to do terraform init,
// RequiredEnvVars object which stores the values required for plan/apply of namespace
func NewApply(opt Options, namespace string) *Apply {
	apply := Apply{
		Options: &opt,
		Applier: NewApplier("/usr/local/bin/terraform", "/usr/local/bin/kubectl"),
		Dir:     "namespaces/" + opt.ClusterDir + "/" + namespace,
	}

	apply.Initialize()
	return &apply
}

// Apply is the entry point for performing a namespace apply.
// It checks if the working directory is in cloud-platform-environments, checks if a PR number or a namespace is given
// If a namespace is given, it perform a kubectl apply and a terraform init and apply of that namespace
// else checks for PR number and get the list of changed namespaces in that merged PR. Then does the kubectl apply and
// terraform init and apply of all the namespaces merged in the PR
func (a *Apply) Apply() error {
	if a.Options.PRNumber == 0 && a.Options.Namespace == "" {
		err := fmt.Errorf("either a PR ID/Number or a namespace is required to perform apply")
		return err
	}
	// If a namespace is given as a flag, then perform a apply for the given namespace.
	if a.Options.Namespace != "" {

		err := a.applyNamespace(a.Options.Namespace)
		if err != nil {
			return err
		}
	} else {
		isMerged, err := a.GithubClient.IsMerged(a.Options.PRNumber)
		if err != nil {
			return err
		}
		if isMerged {
			repos, err := a.GithubClient.GetChangedFiles(a.Options.PRNumber)

			a.Options.OnlySkipFileChanged = false

			if len(repos) == 1 {
				a.Options.OnlySkipFileChanged = strings.Contains(*repos[0].Filename, "APPLY_PIPELINE_SKIP_THIS_NAMESPACE")
			}

			if err != nil {
				return err
			}

			changedNamespaces, err := nsChangedInPR(repos, a.Options.ClusterDir, false)
			if err != nil {
				return err
			}
			for _, namespace := range changedNamespaces {
				if _, err = os.Stat(namespace); err != nil {
					fmt.Println("Applying Namespace:", namespace)
					err = a.applyNamespace(namespace)
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// ApplyAll is the entry point for performing a namespace apply on all namespaces.
// It checks if the working directory is in cloud-platform-environments, get the list of namespace folders and perform kubectl apply
// and terraform init and apply of the namespace
func (a *Apply) ApplyAll() error {
	re := RepoEnvironment{}
	err := re.mustBeInCloudPlatformEnvironments()
	if err != nil {
		return err
	}

	repoPath := "namespaces/" + a.Options.ClusterDir
	folders, err := util.ListFolderPaths(repoPath)
	if err != nil {
		return err
	}

	// skip the root folder namespaces/cluster.cloud-platform.service.justice.gov.uk which is the first
	// element of the slice. We dont want to apply from the root folder
	var nsFolders []string
	nsFolders = append(nsFolders, folders[1:]...)

	err = a.applyNamespaceDirs(nsFolders)
	if err != nil {
		return err
	}

	return nil
}

// ApplyBatch is the entry point for performing a namespace apply on a batch of namespaces.
// It checks if the working directory is in cloud-platform-environments, get the list of namespace folders based on the batch index and size
// and perform kubectl apply and terraform init and apply of the namespace
func (a *Apply) ApplyBatch() error {
	re := RepoEnvironment{}
	err := re.mustBeInCloudPlatformEnvironments()
	if err != nil {
		return err
	}

	repoPath := "namespaces/" + a.Options.ClusterDir
	folderChunks, err := util.GetFolderChunks(repoPath, a.Options.BatchApplyIndex, a.Options.BatchApplySize)
	if err != nil {
		return err
	}

	err = a.applyNamespaceDirs(folderChunks)
	if err != nil {
		return err
	}

	return nil
}

// applyNamespaceDirs get a folder chunk which is the list of namespaces, loop over each of them,
// get the latest changes (In case any PRs were merged since the pipeline started), and perform
// the apply of that namespace
func (a *Apply) applyNamespaceDirs(chunkFolder []string) error {
	erroredNs := []string{}

	done := make(chan bool)
	defer close(done)

	chunkStream := util.Generator(done, chunkFolder...)

	routineResults := a.parallelApplyNamespace(done, chunkStream, 3) // goroutines are very lightweight and can number in millions, but the tasks we are doing are very heavy so we need to limit this as much as possible

	results := util.FanIn(done, routineResults...)

	for res := range results {
		erroredNs = append(erroredNs, res)
	}

	fmt.Printf("\nerrored ns: %v\n", erroredNs)
	return nil
}

func (a *Apply) parallelApplyNamespace(done <-chan bool, dirStream <-chan string, numRoutines int) []<-chan string {
	if a.Options.IsApplyPipeline {
		runtime.GOMAXPROCS(numRoutines) // this is based on https://github.com/ministryofjustice/cloud-platform-infrastructure/blob/ebafd84ba45a18deeb113d1b57f565141368c187/terraform/aws-accounts/cloud-platform-aws/vpc/eks/cluster.tf#L46C1-L46C58 current max cpu is 4 (for workloads running in concourse)
	}

	routineResults := make([]<-chan string, numRoutines)

	for i := 0; i < numRoutines; i++ {
		routineResults[i] = a.runApply(done, dirStream)
	}

	return routineResults
}

func (a *Apply) runApply(done <-chan bool, dirStream <-chan string) <-chan string {
	results := make(chan string)
	go func() {
		defer close(results)
		for dir := range dirStream {
			select {
			case <-done:
				return
			case results <- func(dir string) string {
				ns := strings.Split(dir, "/")
				namespace := ns[2]

				pullErr := util.GetLatestGitPull()
				if pullErr != nil {
					if strings.Contains(pullErr.Error(), "index.lock") {
						fmt.Printf("ignoring git lock error during parallel run\n")
					} else {
						return pullErr.Error()
					}
				}

				err := a.applyNamespace(namespace)
				if err != nil {
					return "Error in namespace: " + namespace + "\n" + err.Error()
				}

				return ""
			}(dir):
			}
		}
	}()

	return results
}

// applyKubectl calls the applier -> applyKubectl with dry-run disabled and return the output from applier
func (a *Apply) applyKubectl() (string, error) {
	log.Printf("Running kubectl for namespace: %v in directory %v", a.Options.Namespace, a.Dir)

	outputKubectl, err := a.Applier.KubectlApply(a.Options.Namespace, a.Dir, false)
	if err != nil {
		err := fmt.Errorf("error running kubectl on namespace %s: %v \n %v", a.Options.Namespace, err, outputKubectl)
		return "", err
	}

	return outputKubectl, nil
}

// deleteKubectl calls the applier -> deleteKubectl with dry-run disabled and return the output from applier
func (a *Apply) deleteKubectl() (string, error) {
	log.Printf("Running kubectl delete for namespace: %v in directory %v", a.Options.Namespace, a.Dir)

	outputKubectl, err := a.Applier.KubectlDelete(a.Options.Namespace, a.Dir, false)
	if err != nil {
		err := fmt.Errorf("error running kubectl delete on namespace %s: %v \n %v", a.Options.Namespace, err, outputKubectl)
		return "", err
	}

	return outputKubectl, nil
}

// applyTerraform calls applier -> TerraformInitAndApply and prints the output from applier
func (a *Apply) applyTerraform() (string, error) {
	log.Printf("Running Terraform Apply for namespace: %v. In directory %v", a.Options.Namespace, a.Dir)

	tfFolder := a.Dir + "/resources"

	if !strings.Contains(a.Dir, a.Options.Namespace) {
		return "", fmt.Errorf("error running terraform as directory and namespace are not aligned Dir=%v and Namespace=%v", a.Dir, a.Options.Namespace)
	}

	outputTerraform, err := a.Applier.TerraformInitAndApply(a.Options.Namespace, tfFolder)
	if err != nil {
		return "", fmt.Errorf("error running terraform on namespace %s: %v \n %v", a.Options.Namespace, err, outputTerraform)
	}
	return outputTerraform, nil
}

// secretBlockerExists takes a filepath (usually a namespace name i.e. namespaces/live.../mynamespace)
// and checks if the file SECRET_ROTATE_BLOCK exists.
func secretBlockerExists(filePath string) bool {
	// Check if the file contains a secret blocker
	// If it doesn't, we do want to apply it
	secretBlocker := "SECRET_ROTATE_BLOCK"
	if _, err := os.Stat(filePath + "/" + secretBlocker); err == nil {
		return true
	}

	return false
}

// applySkipExists takes a filepath (usually a namespace name i.e. namespaces/live.../mynamespace)
// and checks if the file applySkipExists exists.
func applySkipExists(filePath string) bool {
	// Check if the file contains a apply skip, skip applying this namespace
	applySkip := "APPLY_PIPELINE_SKIP_THIS_NAMESPACE"
	if _, err := os.Stat(filePath + "/" + applySkip); err == nil {
		return true
	}

	return false
}

// applyNamespace intiates a new Apply object with options and env variables, and calls the
// applyKubectl with dry-run disabled and calls applier TerraformInitAndApply and prints the output
func (a *Apply) applyNamespace(namespace string) error {
	// secretBlocker is a file used to control the behaviour of a namespace that will have all
	// secrets in a namespace rotated. This came out of the requirement to rotate IAM credentials
	// post circle breach.
	repoPath := "namespaces/" + a.Options.ClusterDir + "/" + namespace

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		fmt.Printf("Namespace %s does not exist, skipping apply\n", namespace)
		return nil
	}

	if secretBlockerExists(repoPath) {
		log.Printf("Namespace %s has a secret rotation blocker file, skipping apply", namespace)
		// We don't want to return an error here so we softly fail.
		return nil
	}

	if (a.Options.EnableApplySkip) && (applySkipExists(repoPath)) {
		log.Printf("Namespace %s has a apply skip file, skipping apply", namespace)
		// We don't want to return an error here so we softly fail.
		return nil
	}

	applier := NewApply(*a.Options, namespace)
	applier.Options.Namespace = namespace

	if util.IsYamlFileExists(repoPath) {
		outputKubectl, err := applier.applyKubectl()
		if err != nil {
			if !a.Options.OnlySkipFileChanged && !a.Options.IsApplyPipeline {
				notifyUserApplyFailed(a.Options.PRNumber, applier.RequiredEnvVars.SlackBotToken, applier.RequiredEnvVars.SlackWebhookUrl, a.Options.BuildUrl)
			}
			return err
		}

		fmt.Println("\nOutput of kubectl:", outputKubectl)
	} else {
		fmt.Printf("Namespace %s does not have yaml resources folder, skipping kubectl apply", namespace)
	}

	exists, err := util.IsFilePathExists(repoPath + "/resources")
	// Set KUBE_CONFIG_PATH to the path of the kubeconfig file
	// This is needed for terraform to be able to connect to the cluster when a different kubecfg is passed
	if err := os.Setenv("KUBE_CONFIG_PATH", a.Options.KubecfgPath); err != nil {
		return err
	}
	if err == nil && exists {
		applier.GithubClient = a.GithubClient
		outputTerraform, err := applier.applyTerraform()
		if err != nil {
			if !a.Options.OnlySkipFileChanged && !a.Options.IsApplyPipeline {
				notifyUserApplyFailed(a.Options.PRNumber, applier.RequiredEnvVars.SlackBotToken, applier.RequiredEnvVars.SlackWebhookUrl, a.Options.BuildUrl)
			}
			return err
		}
		fmt.Printf("\nOutput of terraform for namespace: %s\n", namespace)
		util.RedactedEnv(os.Stdout, outputTerraform, a.Options.RedactedEnv)
	} else {
		fmt.Printf("Namespace %s does not have terraform resources folder, skipping terraform apply", namespace)
	}
	return nil
}

package initfiles

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
	"gopkg.in/yaml.v3"

	age "filippo.io/age"
	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	corev1 "k8s.io/api/core/v1"
)

var errInitialSetup = errors.New("initial environment setup required")

func InitFiles() error {
	for _, step := range []func() error{removeRunAgainFile, ageGen, genRootFiles, genBaseFiles, UpdateRootFiles, UpdateBaseFiles} {
		if err := step(); err != nil {
			if errors.Is(err, errInitialSetup) {
				return nil
			}
			return err
		}
	}
	if err := talosconfig.EnsureSecrets(); err != nil {
		return err
	}
	if err := genKubernetes(); err != nil {
		return err
	}
	if err := GenTalEnvConfigMap(); err != nil {
		return err
	}
	if err := UpdateGitRepo(); err != nil {
		return err
	}
	if err := fluxhandler.CreateGitSecret(helper.TalEnv["GITHUB_REPOSITORY"]); err != nil {
		return fmt.Errorf("create Git deploy secret: %w", err)
	}
	if err := GenSopsSecret(); err != nil {
		return err
	}
	if err := fluxhandler.ProcessDirectory(path.Join(helper.ClusterPath, "kubernetes")); err != nil {
		return err
	}
	if err := fluxhandler.ProcessDirectory(path.Join(helper.ClusterPath, "kubernetes")); err != nil {
		return err
	} else {
		log.Info().Msg("Kustomizations processed successfully.")
	}

	helper.CreateEncrPreCommitHook()
	log.Info().Msg("Init: Completed Successfully!")
	return nil
}

func genKubernetes() error {
	if err := fthelper.CopyDir(helper.KubeCache, helper.ClusterPath+"/kubernetes", false); err != nil {
		return fmt.Errorf("copy Kubernetes files: %w", err)
	}
	if err := fthelper.ReplaceInFile(path.Join(helper.ClusterPath, "kubernetes/flux-entry.yaml"), "REPLACEWITHCLUSTERNAME", helper.ClusterName); err != nil {
		return fmt.Errorf("update Flux entry: %w", err)
	}
	log.Info().Msg("Kubernetes files copied successfully.")
	return nil
}

func GenTalEnvConfigMap() error {
	log.Info().Msg("Creating TalEnv configmap reference 'clustersettings'.")
	// Read the content of the talenv.yaml file
	talenvContent, err := os.ReadFile(helper.ClusterEnvFile)
	if err != nil {
		return err
	}

	// Convert the file content to a string and split it into lines
	talenvLines := strings.Split(string(talenvContent), "\n")

	// Add indentation to each line
	for i, line := range talenvLines {
		talenvLines[i] = "  " + line
	}
	indentClusterName := "  CLUSTERNAME: " + helper.ClusterName
	talenvLines = append(talenvLines, indentClusterName)

	// Join the indented lines back into a single string
	indentedTalenvContent := strings.Join(talenvLines, "\n")

	clusterSettings := filepath.Join("flux-system", "flux", "clustersettings.secret.yaml")
	clusterSettingsDest := filepath.Join(helper.ClusterPath+"/kubernetes", clusterSettings)
	clusterSettingsSrc := filepath.Join(helper.KubeCache, clusterSettings)
	if err := os.MkdirAll(filepath.Join(helper.ClusterPath, "kubernetes", "flux-system", "flux"), os.ModePerm); err != nil {
		return err
	}
	if err := fthelper.CopyFile(clusterSettingsSrc, clusterSettingsDest, true); err != nil {
		return fmt.Errorf("copy cluster settings: %w", err)
	}
	log.Debug().Msgf("clusterSettingsDest %v", clusterSettingsDest)
	err = fthelper.ReplaceInFile(clusterSettingsDest, "REPLACEWITHENV", indentedTalenvContent)
	if err != nil {
		return fmt.Errorf("render cluster settings %s: %w", clusterSettingsDest, err)
	}
	log.Info().Msg("Configmap reference Created.")
	return nil
}

func UpdateGitRepo() error {
	if helper.TalEnv["GITHUB_REPOSITORY"] != "" {
		repoPath := filepath.Join("repositories", "git", "this-repo.yaml")
		if _, err := os.Stat(repoPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				log.Warn().Msgf("Skipping Git repository update: %s does not exist", filepath.ToSlash(repoPath))
				return nil
			}
			return fmt.Errorf("check Git repository file %s: %w", repoPath, err)
		}
		gitrepo := FormatGitURL(helper.TalEnv["GITHUB_REPOSITORY"])
		if err := fthelper.ReplaceInFile(repoPath, "ssh://REPLACEWITHGITREPO", gitrepo); err != nil {
			return fmt.Errorf("update Git repository file %s: %w", repoPath, err)
		}
	}
	return nil
}

// FormatGitURL formats the input Git URL according to the specified rules.
func FormatGitURL(input string) string {
	// Remove "https://" prefix if present
	input = strings.TrimPrefix(input, "https://")

	if !strings.HasPrefix(input, "ssh://") {
		input = "ssh://" + input
	}

	// Ensure input starts with "ssh://git@"
	if !strings.HasPrefix(input, "ssh://git@") {
		// Prepend "ssh://git@" if neither "ssh://" nor "git@" is present
		input = strings.Replace(input, "ssh://", "ssh://git@", 1)
	}

	if strings.Contains(input, "git@git@") {
		input = strings.Replace(input, "git@git@", "git@", 1)
	}

	// Compile a regex to match and replace the URL pattern
	re := regexp.MustCompile(`^ssh://git@([^:/]+)([:/])([\w-]+)/([\w-]+)\.git$`)
	matches := re.FindStringSubmatch(input)

	if len(matches) == 5 {
		// Determine the user and repo based on the separator used
		user := matches[3] // Always captured as part of the matched group
		repo := matches[4] // Always captured as part of the matched group
		return fmt.Sprintf("ssh://git@%s/%s/%s.git", matches[1], user, repo)
	}

	return input // Return the input as is if it doesn't match
}

func genBaseFiles() error {
	clusterEnvPresent := false

	if _, err := os.Stat(helper.ClusterEnvFile); err == nil {
		clusterEnvPresent = true
		log.Debug().Msg("Detected existing cluster, continuing")
	} else if os.IsNotExist(err) {
		if err := createRunAgainFile(); err != nil {
			return err
		}
		log.Warn().Msg("New cluster detected, creating clusterenv.yaml\n Please fill out ClusterEnv.yaml and run init again, after setting-up clusterenv.yaml!")
	} else {
		return fmt.Errorf("check cluster environment %s: %w", helper.ClusterEnvFile, err)
	}

	err := fthelper.CopyDir(helper.BaseCache, helper.ClusterPath+"", false)
	if err != nil {
		return err
	} else {
		log.Info().Msg("Base files copied successfully.")
	}

	if !clusterEnvPresent {
		return errInitialSetup
	}

	log.Info().Msg("basefiles successfully altered.")
	return nil
}

// Create the "RUNAGAIN" file
func createRunAgainFile() error {
	file, err := os.Create("RUNAGAIN")
	if err != nil {
		return fmt.Errorf("create RUNAGAIN: %w", err)
	}
	return file.Close()
}

// Remove the "RUNAGAIN" file if it exists
func removeRunAgainFile() error {
	if CheckRunAgainFileExists() {
		err := os.Remove("RUNAGAIN")
		if err != nil {
			return fmt.Errorf("remove RUNAGAIN: %w", err)
		}
		log.Debug().Msg("RUNAGAIN file removed.")
	} else {
		log.Debug().Msg("RUNAGAIN file does not exist.")
	}
	return nil
}

// Check if the "RUNAGAIN" file exists
func CheckRunAgainFileExists() bool {
	_, err := os.Stat("RUNAGAIN")
	return !os.IsNotExist(err)
}

func UpdateBaseFiles() error {
	log.Info().Msgf("Updating base files for cluster: %s", helper.ClusterPath)
	// Read filenames in source directory
	sourceFiles, err := readFilenamesInDir(helper.BaseCache)
	if err != nil {
		return fmt.Errorf("read template directory: %w", err)
	}

	// Process each file in the target directory
	for _, filename := range sourceFiles {
		sourceFilePath := filepath.Join(helper.BaseCache, filename)
		targetFilePath := filepath.Join(helper.ClusterPath+"", fthelper.ReplaceDotInFilename(filename))
		if err := fthelper.ReplaceContentBetweenLines(targetFilePath, sourceFilePath, "## Do not edit between this and DO NOT REMOVE", "## DO NOT REMOVE: Personal setting go under this line"); err != nil {
			return fmt.Errorf("update %s: %w", targetFilePath, err)
		}
	}
	log.Info().Msg("basefiles successfully updated.")

	if err := CheckEnvVariables(); err != nil {
		return err
	}

	return nil

}

func genRootFiles() error {
	if err := fthelper.CopyDir(helper.RootCache, "./", false); err != nil {
		return fmt.Errorf("copy root files: %w", err)
	}
	agePubKey, err := GetPubKey()
	if err != nil {
		return fmt.Errorf("read age public key: %w", err)
	}
	if err := fthelper.ReplaceInFile(".sops.yaml", "REPLACEME", agePubKey); err != nil {
		return fmt.Errorf("configure .sops.yaml: %w", err)
	}
	log.Info().Msg("Root files copied successfully.")
	return nil
}

func UpdateRootFiles() error {
	// Read filenames in source directory
	sourceFiles, err := readFilenamesInDir(helper.RootCache)
	if err != nil {
		return fmt.Errorf("read template directory: %w", err)
	}

	// Process each file in the target directory
	for _, filename := range sourceFiles {
		sourceFilePath := filepath.Join(helper.RootCache, filename)
		targetFilePath := filepath.Join("./", fthelper.ReplaceDotInFilename(filename))
		if err := fthelper.ReplaceContentBetweenLines(targetFilePath, sourceFilePath, "## Do not edit between this and DO NOT REMOVE", "## DO NOT REMOVE: Personal setting go under this line"); err != nil {
			return fmt.Errorf("update %s: %w", targetFilePath, err)
		}
	}
	log.Info().Msg("rootfiles successfully updated.")

	agePubKey, err := GetPubKey()
	if err != nil {
		return fmt.Errorf("read age public key: %w", err)
	}

	err = fthelper.ReplaceInFile(".sops.yaml", "REPLACEME", agePubKey)
	if err != nil {
		return fmt.Errorf("configure .sops.yaml: %w", err)
	}

	if err := CheckEnvVariables(); err != nil {
		return err
	}

	return nil

}

// Function to read all filenames in a directory
func readFilenamesInDir(dir string) ([]string, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var filenames []string
	for _, file := range files {
		if !file.IsDir() {
			filenames = append(filenames, file.Name())
		}
	}
	return filenames, nil
}

func ResetBootstrapValues() error {
	if err := LoadTalEnv(false); err != nil {
		return err
	}
	if err := fthelper.CopyDirFiltered(helper.KubeCache, helper.ClusterPath+"/kubernetes", true, `^bootstrap-values\.yaml.ct$`); err != nil {
		return fmt.Errorf("copy bootstrap values: %w", err)
	}
	if err := fthelper.EnvSubstRecursive(helper.ClusterPath+"/kubernetes", `^bootstrap-values\.yaml.ct$`, helper.TalEnv); err != nil {
		return fmt.Errorf("render bootstrap values: %w", err)
	}
	log.Info().Msg("Bootstrap values reset successfully.")
	return nil
}

func ageGen() error {
	const filename = "age.agekey"
	if _, err := os.Stat(filename); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check age key: %w", err)
	}
	key, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate age key: %w", err)
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create age key: %w", err)
	}
	_, writeErr := fmt.Fprintf(f, "# created: %s\n# public key: %s\n%s\n", time.Now().Format(time.RFC3339), key.Recipient(), key)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write age key: %w", err)
	}
	return nil
}

func GetPubKey() (string, error) {
	// Open the file
	filename := "age.agekey"
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var publicKey string

	// Read the file line by line
	for scanner.Scan() {
		line := scanner.Text()
		// Find the line with the public key
		if strings.HasPrefix(line, "# public key:") {
			parts := strings.Split(line, ": ")
			if len(parts) == 2 {
				publicKey = parts[1]
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan file: %v", err)
	}

	if publicKey == "" {
		return "", fmt.Errorf("public key not found")
	}

	return publicKey, nil
}

// getSecretKeyFromFile reads the specified file and returns the secret key found within it.
func GetSecKey() (string, error) {
	// Open the file
	filename := "age.agekey"
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var secretKey string

	// Read the file line by line
	for scanner.Scan() {
		line := scanner.Text()
		// Find the line that contains the secret key prefix
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			secretKey = line
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan file: %v", err)
	}

	if secretKey == "" {
		return "", fmt.Errorf("secret key not found")
	}

	return secretKey, nil
}

func GenSopsSecret() error {
	secretPath := filepath.Join(helper.ClusterPath, "kubernetes", "flux-system", "flux", "sopssecret.secret.yaml")
	ageSecKey, err := GetSecKey()

	// Added by Boemeltrein, for linting purposes
	if err != nil {
		return fmt.Errorf("failed to get age secret key: %w", err)
	}

	// Generate Kubernetes secret YAML content
	secret := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      "sops-age",
			"namespace": "flux-system",
		},
		"stringData": map[string]interface{}{
			"age.agekey": ageSecKey,
		},
		"type": string(corev1.SecretTypeOpaque),
	}

	secretYAML, err := yaml.Marshal(secret)
	if err != nil {
		return fmt.Errorf("failed to marshal secret to YAML: %w", err)
	}

	// Write Kubernetes secret YAML to file
	err = os.MkdirAll(filepath.Dir(secretPath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}
	err = os.WriteFile(secretPath, secretYAML, 0644)
	if err != nil {
		return fmt.Errorf("failed to write secret YAML to file: %w", err)
	}
	log.Info().Msgf("SOPS secret YAML saved to: %s\n", secretPath)
	return nil
}

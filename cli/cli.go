/*
The MIT License (MIT)

Copyright (c) 2022-2023 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/
package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	ShellToUse   = "sh"
	KubesealApp  = "tools/kubeseal"
	RelizaCliApp = "tools/reliza-cli"
	HelmMimeType = "application/vnd.cncf.helm.config.v1+json"
)

var (
	sugar            *zap.SugaredLogger
	SecretsNamespace string
	RelizaNamespace  string
	watcherImage     string
	enableWatcher    bool
	argoInfo         ArgoInfo
	EnvMode          string
	DryRun           bool
)

const (
	StandaloneMode   = "STANDALONE"
	ExistingArgoMode = "EXISTING_ARGO"
	NewArgoMode      = "NEW_ARGO"
)

func init() {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	var logger, _ = config.Build()
	defer logger.Sync()
	sugar = logger.Sugar()

	if len(os.Getenv("MY_NAMESPACE")) > 0 {
		RelizaNamespace = os.Getenv("MY_NAMESPACE")
	} else {
		RelizaNamespace = "argocd"
	}
	SecretsNamespace = RelizaNamespace

	if len(os.Getenv("WATCHER_IMAGE")) > 0 {
		watcherImage = os.Getenv("WATCHER_IMAGE")
	} else {
		watcherImage = "relizaio/reliza-watcher"
	}
	enableWatcher = true
	if len(os.Getenv("ENABLE_WATCHER")) > 0 {
		enableWatcherStr := os.Getenv("ENABLE_WATCHER")
		if strings.ToLower(enableWatcherStr) == "false" {
			enableWatcher = false
		}
	}
	if len(os.Getenv("MODE")) > 0 {
		EnvMode = os.Getenv("MODE")
	} else {
		EnvMode = "STANDALONE"
	}

	DryRun = false
	if strings.ToLower(os.Getenv("DRY_RUN")) == "true" {
		DryRun = true
	}

	if DryRun {
		sugar.Info("DRY_RUN mode is enabled - mutating helm/kubectl commands will be logged but not executed")
	}
	sugar.Info("Running Reliza CD in " + EnvMode + " mode.")
	argoInfo = detectArgo()
	if EnvMode == NewArgoMode && !argoInfo.IsArgoDetected {
		installArgoCD()
		argoInfo = detectArgo()
	} else if EnvMode == NewArgoMode && argoInfo.IsArgoDetected {
		sugar.Info("argocd Installation found, skiping new install ..")
	}

	if !argoInfo.IsArgoDetected && EnvMode != StandaloneMode {
		sugar.Error("Mode is set to `" + EnvMode + "` but no argo installation detected on the cluster!")
		panic("Mode is set to `" + EnvMode + "` but no argo installation detected on the cluster!")
	}

	if argoInfo.IsArgoEnabled {
		SecretsNamespace = argoInfo.ArgoNamespace
	}
}

func shellout(command string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Dir = "/app" // Set working directory to /app where tools are located
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err != nil {
		sugar.Error("stdout: ", stdout.String(), "stderr: ", stderr.String(), "error: ", err.Error())
	}

	return stdout.String(), stderr.String(), err
}

func dryRunShellout(command string) (string, string, error) {
	if DryRun {
		sugar.Info("DRY_RUN: would execute: ", command)
		return "", "", nil
	}
	return shellout(command)
}

func SetSealedCertificateOnTheHub(cert string) {
	certPath := "workspace/sealedCert.pem"
	doSet := false
	existingCert, err := os.ReadFile(certPath)
	if err != nil && os.IsNotExist(err) {
		doSet = true
	} else if err != nil {
		doSet = true
		sugar.Error(err)
	} else if strings.Compare(cert, string(existingCert)) != 0 {
		doSet = true
	}

	if doSet {
		sugar.Info("Setting Bitnami Sealed Certificate on Reliza Hub")
		_, _, err := shellout(RelizaCliApp + " cd setsecretcert --cert " + cert)
		if err == nil {
			err := os.RemoveAll(certPath)
			if err != nil {
				sugar.Error(err)
			}
			certCheckFile, err := os.Create(certPath)
			if err != nil {
				sugar.Error(err)
			}
			certCheckFile.WriteString(cert)
			err = certCheckFile.Close()
			if err != nil {
				sugar.Error(err)
			}
		}
		sugar.Info("Set Bitnami Sealed Certificate on Reliza Hub")
	}
}

func GetInstanceCycloneDX() (string, error) {
	instManifest, _, err := shellout(RelizaCliApp + " exportinst")
	return instManifest, err
}

func ExtractRlzDigestFromCdxDigest(cdxHash cdx.Hash) string {
	algstr := strings.ToLower(string(cdxHash.Algorithm))
	algstr = strings.Replace(algstr, "-", "", -1)
	return algstr + ":" + cdxHash.Value
}

func GetSealedCert() string {
	fetchCertArg := "--fetch-cert | base64 -w 0"
	sugar.Debug("Fetching sealed cert with command: ", KubesealApp+" "+fetchCertArg)
	cert, stderr, err := shellout(KubesealApp + " " + fetchCertArg)
	if err != nil {
		sugar.Error("Error fetching sealed cert: ", err, " stderr: ", stderr)
	}
	sugar.Debug("Sealed cert fetched, length: ", len(cert))
	return cert
}

func resolveDeploymentNameFromString(origName string) string {
	rdName := strings.ToLower(origName)
	rdName = strings.ReplaceAll(rdName, " ", "-")
	return rdName
}

// dockerTagSafeVersion converts a version string to be Docker tag safe
// by replacing characters that are not allowed in Docker tags with underscores.
// Docker tags must match: [\w][\w.-]{0,127}
func dockerTagSafeVersion(baseVersion string) string {
	// Replace any character that is not alphanumeric, dot, or hyphen with underscore
	re := regexp.MustCompile(`[^\w.-]`)
	return re.ReplaceAllString(baseVersion, "_")
}

func produceAppConfigMapFromCdxComponents(cdxComponents *[]cdx.Component) map[string]appConfig {
	appConfigMap := make(map[string]appConfig)
	if nil != cdxComponents && len(*cdxComponents) > 0 {
		for _, comp := range *cdxComponents {
			if comp.Type == "application" {
				var appConfig appConfig
				appConfig.ValuesFile = "values.yaml"

				// Check for HELM_APP_VERSION property (for external Helm charts)
				helmAppVersion := ""
				if comp.Properties != nil && len(*comp.Properties) > 0 {
					for _, prop := range *comp.Properties {
						if prop.Name == "CONFIGURATION" && prop.Value != "default" {
							appConfig.ValuesFile = prop.Value
						} else if prop.Name == "HELM_APP_VERSION" {
							// HELM_APP_VERSION: For external Helm charts (e.g., Jenkins 2.375.2)
							// This is the actual application version inside the Helm chart
							helmAppVersion = prop.Value
						}
					}
				}

				// Set appVersion for Chart.yaml metadata
				if helmAppVersion != "" {
					// External Helm charts: use the extracted app version (e.g., Jenkins 2.375.2)
					appConfig.AppVersion = helmAppVersion
				} else if comp.Version != "" {
					// Your own products: use the product version directly (e.g., Reliza 25.03.0.13)
					// No formatting needed - this is just metadata in Chart.yaml
					appConfig.AppVersion = comp.Version
				} else {
					// No version found - this is an error condition
					sugar.Error(fmt.Sprintf("No version found for application component %s (group: %s). Cannot proceed without a valid version.", comp.Name, comp.Group))
					os.Exit(1)
				}

				deplName := resolveDeploymentNameFromString(comp.Group)
				appConfigMap[deplName] = appConfig
			}
		}
	}
	return appConfigMap
}

func ParseInstanceCycloneDXIntoDeployments(cyclonedxManifest string) []RelizaDeployment {
	bom := new(cdx.BOM)
	manifestReader := strings.NewReader(cyclonedxManifest)
	decoder := cdx.NewBOMDecoder(manifestReader, cdx.BOMFileFormatJSON)
	if err := decoder.Decode(bom); err != nil {
		sugar.Error(err)
	}

	var rlzDeployments []RelizaDeployment

	appConfigMap := produceAppConfigMapFromCdxComponents(bom.Components)

	if nil != bom.Components && len(*bom.Components) > 0 {
		for _, comp := range *bom.Components {
			if comp.MIMEType == HelmMimeType {
				var rd RelizaDeployment
				rd.Name = resolveDeploymentNameFromString(comp.Group)
				namespaceBundle := strings.Split(comp.Group, "---")
				rd.Namespace = namespaceBundle[0]
				rd.Bundle = namespaceBundle[1]
				rd.ArtUri = comp.Name
				rd.ArtVersion = comp.Version
				appConfig := appConfigMap[rd.Name]
				configFile := "values.yaml"
				if len(appConfig.ValuesFile) > 0 {
					configFile = appConfig.ValuesFile
				}
				rd.ConfigFile = configFile
				appVersion := ""
				if len(appConfig.AppVersion) > 0 {
					appVersion = appConfig.AppVersion
					rd.AppVersion = appVersion
				}
				if comp.Hashes != nil && len(*comp.Hashes) > 0 {
					hashes := *comp.Hashes
					rd.ArtHash = hashes[0]
				} else {
					// Helm charts may not have hashes - use empty hash for public repos
					sugar.Debug("No hash found for Helm artifact = " + rd.ArtUri + ", assuming public repository")
					rd.ArtHash = cdx.Hash{Algorithm: cdx.HashAlgoSHA256, Value: ""}
				}
				rlzDeployments = append(rlzDeployments, rd)
			}
		}
	}

	return rlzDeployments
}

func GetProjectAuthByArtifactDigest(artDigest, releaseNamespace string) ProjectAuth {
	//TODO: use --releasens when api is updated
	authResp, _, _ := shellout(RelizaCliApp + " cd artsecrets --artdigest " + artDigest + " --namespace " + SecretsNamespace + " --instanceuri " + releaseNamespace)
	var projectAuth map[string]ProjectAuth
	json.Unmarshal([]byte(authResp), &projectAuth)
	return projectAuth["artifactDownloadSecrets"]
}

func ProduceSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string, helmInfo HelmRepoInfo) {
	secretTmpl :=
		`apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  annotations:
    sealedsecrets.bitnami.com/namespace-wide: "true"
  labels:
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
    
spec:
  encryptedData:
    username: {{.Username}}
    password: {{.Password}}
  template:
    data:
      url: {{.Url}}
      name: {{.Name}}
      type: helm
      enableOCI: {{printf "%q" .EnableOci}}
    metadata:
      labels:
        reliza.io/type: cdresource
        reliza.io/name: {{.Name}}
        argocd.argoproj.io/secret-type: repository`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = rd.Name
	secTmplRes.Namespace = namespace
	secTmplRes.Username = projAuth.Login
	secTmplRes.Password = projAuth.Password
	secTmplRes.Url = helmInfo.RepoHost
	secTmplRes.EnableOci = strconv.FormatBool(helmInfo.UseOci)
	if helmInfo.UseOci {
		secTmplRes.Url = helmInfo.RepoHost
	} else {
		secTmplRes.Url = helmInfo.RepoUri
	}
	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func ProducePlainSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string, helmInfo HelmRepoInfo) {
	sugar.Info("ProducePlainSecretYaml - helmInfo: ", helmInfo)
	secretTmpl :=
		`apiVersion: v1
kind: Secret
metadata:
  labels:
    argocd.argoproj.io/secret-type: repository
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
  name: {{.Name}}
  namespace: {{.Namespace}}
type: Opaque
data:
  type: aGVsbQ==
  url: {{.Url}}
  name: {{.NameBase64}}
  username: {{.Username}}
  password: {{.Password}}
  enableOCI: {{printf "%q" .EnableOci}}`
	sugar.Info("ProducePlainSecretYaml - EnableOci: ", helmInfo.UseOci)
	sugar.Info("ProducePlainSecretYaml - Url: ", helmInfo.RepoHost)
	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = rd.Name
	secTmplRes.NameBase64 = base64.StdEncoding.EncodeToString([]byte(rd.Name))
	secTmplRes.Namespace = namespace
	secTmplRes.Username = base64.StdEncoding.EncodeToString([]byte(projAuth.Login))
	secTmplRes.Password = base64.StdEncoding.EncodeToString([]byte(projAuth.Password))
	secTmplRes.EnableOci = base64.StdEncoding.EncodeToString([]byte(strconv.FormatBool(helmInfo.UseOci)))
	if helmInfo.UseOci {
		secTmplRes.Url = base64.StdEncoding.EncodeToString([]byte(helmInfo.RepoHost))
	} else {
		secTmplRes.Url = base64.StdEncoding.EncodeToString([]byte(helmInfo.RepoUri))
	}
	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func ProduceNoCredSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string, helmInfo HelmRepoInfo) {
	secretTmpl :=
		`apiVersion: v1
kind: Secret
metadata:
  labels:
    argocd.argoproj.io/secret-type: repository
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
  name: {{.Name}}
  namespace: {{.Namespace}}
type: Opaque
data:
  type: aGVsbQ==
  url: {{.Url}}
  name: {{.NameBase64}}
  enableOCI: {{printf "%q" .EnableOci}}`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = rd.Name
	secTmplRes.NameBase64 = base64.StdEncoding.EncodeToString([]byte(rd.Name))
	secTmplRes.Namespace = namespace
	secTmplRes.Username = base64.StdEncoding.EncodeToString([]byte(projAuth.Login))
	secTmplRes.Password = base64.StdEncoding.EncodeToString([]byte(projAuth.Password))
	secTmplRes.EnableOci = base64.StdEncoding.EncodeToString([]byte(strconv.FormatBool(helmInfo.UseOci)))
	if helmInfo.UseOci {
		secTmplRes.Url = base64.StdEncoding.EncodeToString([]byte(helmInfo.RepoHost))
	} else {
		secTmplRes.Url = base64.StdEncoding.EncodeToString([]byte(helmInfo.RepoUri))
	}

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func ProduceEcrSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string) {
	secretTmpl :=
		`apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  annotations:
    sealedsecrets.bitnami.com/namespace-wide: "true"
  labels:
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
spec:
  encryptedData:
    username: {{.Username}}
    password: {{.Password}}
  template:
    data:
      url: {{.Url}}
    metadata:
      labels:
        reliza.io/name: {{.Name}}
        reliza.io/type: cdresource`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = "ecr-" + rd.Name
	secTmplRes.Namespace = namespace
	secTmplRes.Username = projAuth.Login
	secTmplRes.Password = projAuth.Password
	secTmplRes.Url = rd.ArtUri

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func WaitUntilSecretCreated(name string, namespace string) {
	secretWaitCmd := "while ! " + KubectlApp + " get secret " + name + " -n " + namespace + "; do sleep 1; done"
	shellout(secretWaitCmd)
}

func IsFirstInstallDone(rd *RelizaDeployment) bool {
	isFirstInstallDone := false

	if argoInfo.IsArgoEnabled {
		isFirstInstallDone = IsFirstArgoInstallDone(rd)
	}

	if !isFirstInstallDone {
		isFirstInstallDone = IsFirstHelmInstallDone(rd)
	}

	return isFirstInstallDone
}

func InstallApplication(groupPath string, rd *RelizaDeployment) error {
	var err error

	if argoInfo.IsArgoEnabled {
		err = installArgoApplication(groupPath, rd, argoInfo.ArgoNamespace)
	} else {
		err = InstallHelmChart(groupPath, rd)
	}
	return err
}

type SecretTemplateResolver struct {
	Name       string
	Namespace  string
	Username   string
	Password   string
	Url        string
	NameBase64 string
	EnableOci  string
}

type RelizaDeployment struct {
	Name       string
	Namespace  string
	Bundle     string
	ArtUri     string
	ArtVersion string
	ArtHash    cdx.Hash
	ConfigFile string
	AppVersion string
}

type ProjectAuth struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Type     string `json:"type"`
	Url      string
}

type appConfig struct {
	ValuesFile string
	AppVersion string
}

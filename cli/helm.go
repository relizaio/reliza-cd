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
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	HelmApp               = "tools/helm"
	KubectlApp            = "tools/kubectl"
	WorkValues            = "work-values.yaml"
	ValuesDiff            = "values-diff.yaml"
	ValuesDiffPrev        = "values-diff-prev.yaml"
	LastVersionFile       = "last_version"
	InstallValues         = "install-values.yaml"
	RecordedDeloyedData   = "recorded-deployed-data.json"
	WatcherHelmDataSuffix = "-watcher-helm.json"
	CustomValuesFile      = "reliza-hub-custom-values.yaml"
)

func InstallSealedCertificates() {
	sugar.Info("Installing Bitnami Sealed Certificate")
	// https://github.com/bitnami-labs/sealed-secrets#helm-chart
	shellout(HelmApp + " install sealed-secrets -n kube-system --set-string fullnameOverride=sealed-secrets-controller oci://registry.relizahub.com/library/sealed-secrets")
}

func ResolveHelmAuthSecret(secretName string) ProjectAuth {
	var pa ProjectAuth
	username, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.username} -n " + SecretsNamespace + " | base64 -d")
	password, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.password} -n " + SecretsNamespace + " | base64 -d")
	url, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.url} -n " + SecretsNamespace + " | base64 -d")
	pa.Type = "CREDS"
	if strings.Contains(url, ".dkr.ecr.") && strings.Contains(url, "amazonaws.com") {
		pa.Type = "ECR"
	}
	pa.Url = url
	pa.Login = username
	pa.Password = password
	return pa
}

func KubectlApply(path string) {
	dryRunShellout(KubectlApp + " apply -f " + path)
}

func cleanupHelmChart(helmChartPath string) {
	shellout("rm -rf " + helmChartPath + "/")
	shellout("rm -rf " + helmChartPath + "*.tgz")
}

func GetHelmRepoInfoFromDeployment(rd *RelizaDeployment) HelmRepoInfo {
	var helmRepoInfo HelmRepoInfo

	helmRepoInfo.ChartName = GetChartNameFromDeployment(rd)
	helmRepoInfo.RepoUri = strings.Replace(rd.ArtUri, "/"+helmRepoInfo.ChartName, "", -1)

	// Determine if this is an OCI registry based on domain patterns or oci:// prefix
	helmRepoInfo.UseOci = false
	if strings.Contains(rd.ArtUri, "oci://") {
		helmRepoInfo.UseOci = true
		helmRepoInfo.OciUri = rd.ArtUri
	} else if strings.Contains(rd.ArtUri, "azurecr.io") || strings.Contains(rd.ArtUri, ".ecr.") || strings.Contains(rd.ArtUri, ".pkg.dev") || (strings.Contains(rd.ArtUri, ".relizahub.com") && !strings.Contains(rd.ArtUri, "/chartrepo/")) {
		helmRepoInfo.UseOci = true
	}

	// Add protocol if missing
	if !strings.HasPrefix(helmRepoInfo.RepoUri, "http://") && !strings.HasPrefix(helmRepoInfo.RepoUri, "https://") && !strings.HasPrefix(helmRepoInfo.RepoUri, "oci://") {
		if helmRepoInfo.UseOci {
			// For OCI registries without protocol, add oci://
			helmRepoInfo.RepoUri = "oci://" + helmRepoInfo.RepoUri
			helmRepoInfo.OciUri = helmRepoInfo.RepoUri + "/" + helmRepoInfo.ChartName
		} else {
			// For traditional Helm repos without protocol, add https://
			helmRepoInfo.RepoUri = "https://" + helmRepoInfo.RepoUri
		}
	}

	helmRepoInfo.RepoHost = strings.Replace(helmRepoInfo.RepoUri, "https://", "", -1)
	helmRepoInfo.RepoHost = strings.Replace(helmRepoInfo.RepoHost, "http://", "", -1)
	helmRepoInfo.RepoHost = strings.Replace(helmRepoInfo.RepoHost, "oci://", "", -1)

	// Construct OciUri if needed and not already set
	if helmRepoInfo.UseOci && helmRepoInfo.OciUri == "" {
		helmRepoInfo.OciUri = "oci://" + helmRepoInfo.RepoHost + "/" + helmRepoInfo.ChartName
	}

	return helmRepoInfo
}

type HelmRepoInfo struct {
	ChartName string
	RepoUri   string
	RepoHost  string
	UseOci    bool
	OciUri    string
}

func DownloadHelmChart(path string, rd *RelizaDeployment, pa *ProjectAuth, helmRepoInfo HelmRepoInfo) error {
	var err error
	cleanupHelmChart(path + helmRepoInfo.ChartName)

	if helmRepoInfo.UseOci {

		if pa.Type != "NOCREDS" {
			_, _, err = shellout(HelmApp + " registry login " + helmRepoInfo.RepoHost + " --username " + pa.Login + " --password " + pa.Password)
		}
		if err == nil {
			_, _, err = shellout(HelmApp + " pull " + helmRepoInfo.OciUri + " --version " + rd.ArtVersion + " -d " + path)
		}
	} else {
		if pa.Type != "NOCREDS" {
			_, _, err = shellout(HelmApp + " repo add " + helmRepoInfo.ChartName + " " + helmRepoInfo.RepoUri + " --force-update --username " + pa.Login + " --password " + pa.Password)
		} else {
			_, _, err = shellout(HelmApp + " repo add " + helmRepoInfo.ChartName + " " + helmRepoInfo.RepoUri + " --force-update")
		}
		if err == nil {
			shellout(HelmApp + " repo update " + helmRepoInfo.ChartName)
			shellout(HelmApp + " pull " + helmRepoInfo.ChartName + "/" + helmRepoInfo.ChartName + " --version " + rd.ArtVersion + " -d " + path)
		}
	}
	if err == nil {
		_, _, err = shellout("tar -xzvf " + path + "*.tgz -C " + path)
	}
	if err != nil {
		sugar.Errorw("Failed to download helm chart",
			"bundle", rd.Bundle,
			"version", rd.ArtVersion,
			"chartName", helmRepoInfo.ChartName,
			"namespace", rd.Namespace,
			"repoUri", helmRepoInfo.RepoUri,
			"error", err)
	}
	return err
}

func resolveCustomValuesFromHub(groupPath string, rd *RelizaDeployment) bool {
	present := false
	custValCmd := RelizaCliApp + " instprops --property CUSTOM_VALUES --usenamespacebundle=true --namespace " + rd.Namespace + " --bundle '" + rd.Bundle + "'"
	sugar.Debug("Fetching CUSTOM_VALUES for bundle: ", rd.Bundle, " namespace: ", rd.Namespace)
	sugar.Debug("Command: ", custValCmd)
	propsFromCli, stderr, err := shellout(custValCmd)
	if err != nil {
		sugar.Error("Failed to fetch CUSTOM_VALUES: ", err)
		sugar.Error("stderr: ", stderr)
		return false
	}
	sugar.Debug("custValues response = ", propsFromCli)
	var secretPropsResp SecretPropsCliResponse
	unmarshalErr := json.Unmarshal([]byte(propsFromCli), &secretPropsResp)
	if unmarshalErr != nil {
		sugar.Error("Failed to unmarshal CUSTOM_VALUES response: ", unmarshalErr)
		return false
	}

	custValues := ""
	if len(secretPropsResp.Properties) > 0 {
		prop := secretPropsResp.Properties[0]
		custValues = prop.Value
		sugar.Debug("CUSTOM_VALUES found, length: ", len(custValues), " bytes")
	} else {
		sugar.Debug("No CUSTOM_VALUES found for bundle: ", rd.Bundle, " namespace: ", rd.Namespace)
	}

	if len(custValues) > 0 {
		helmChartName := GetChartNameFromDeployment(rd)
		customValuesFilePath := groupPath + helmChartName + "/" + CustomValuesFile
		sugar.Debug("Writing CUSTOM_VALUES to: ", customValuesFilePath)
		shellout("rm -rf " + customValuesFilePath)
		customValuesFile, err := os.Create(customValuesFilePath)
		if err != nil {
			sugar.Error("Failed to create custom values file: ", err)
		} else {
			bytesWritten, writeErr := customValuesFile.WriteString(custValues)
			if writeErr != nil {
				sugar.Error("Failed to write custom values: ", writeErr)
			} else {
				sugar.Debug("Wrote ", bytesWritten, " bytes to custom-values.yaml")
			}
			customValuesFile.Close()
			present = true
		}
	}

	return present
}

func MergeHelmValues(groupPath string, rd *RelizaDeployment) error {
	sugar.Info("=== Starting Helm Values Merge ===")
	hasCustomValues := resolveCustomValuesFromHub(groupPath, rd)
	helmChartName := GetChartNameFromDeployment(rd)
	valuesFlags := " -f " + rd.ConfigFile
	if hasCustomValues {
		valuesFlags += " -f " + CustomValuesFile
		sugar.Debug("Merging with CUSTOM_VALUES")
	} else {
		sugar.Debug("No CUSTOM_VALUES to merge, using default values only")
	}
	helmValuesCmd := RelizaCliApp + " helmvalues " + groupPath + helmChartName + valuesFlags + " --outfile " + groupPath + WorkValues
	sugar.Info("Merge command: ", helmValuesCmd)
	stdout, stderr, err := shellout(helmValuesCmd)
	if err != nil {
		sugar.Error("Failed to merge helm values: ", err)
		sugar.Error("stdout: ", stdout)
		sugar.Error("stderr: ", stderr)
	} else {
		sugar.Info("✅ Helm values merged successfully")
	}
	return err
}

func ResolvePreviousDiffFile(groupPath string) error {
	os.RemoveAll(groupPath + ValuesDiffPrev)
	_, _, err := shellout("cp " + groupPath + ValuesDiff + " " + groupPath + ValuesDiffPrev +
		" || echo 'no prev values file present yet' > " + groupPath + ValuesDiffPrev)
	return err
}

func ReplaceTagsForDiff(groupPath string, namespace string) error {
	replaceTagsCmd := RelizaCliApp + " replacetags --infile " + groupPath + WorkValues + " --outfile " + groupPath + ValuesDiff + " --fordiff=true --resolveprops=true --namespace " + namespace
	_, _, err := shellout(replaceTagsCmd)
	return err
}

func ReplaceTagsForInstall(groupPath string, namespace string) error {
	replaceTagsCmd := RelizaCliApp + " replacetags --infile " + groupPath + WorkValues + " --outfile " + groupPath + InstallValues + " --resolveprops=true --namespace " + namespace
	sugar.Info("Replacing tags for install, command: ", replaceTagsCmd)
	stdout, stderr, err := shellout(replaceTagsCmd)
	if err != nil {
		sugar.Error("Failed to replace tags: ", err)
		sugar.Error("stdout: ", stdout)
		sugar.Error("stderr: ", stderr)
	} else {
		sugar.Info("✅ Tags replaced successfully, install-values.yaml created")
	}
	return err
}

func IsValuesDiff(groupPath string) bool {
	isDiff := false
	prevVal, err := os.ReadFile(groupPath + ValuesDiffPrev)
	if err != nil && os.IsNotExist(err) {
		isDiff = true
	} else if err != nil {
		sugar.Error(err)
	}

	if !isDiff {
		newVal, err := os.ReadFile(groupPath + ValuesDiff)
		if err != nil {
			sugar.Error(err)
		}

		if 0 != strings.Compare(string(newVal), string(prevVal)) {
			isDiff = true
		}
	}
	return isDiff
}

func IsFirstHelmInstallDone(rd *RelizaDeployment) bool {
	isFirstInstallDone := false
	helmChartName := GetChartNameFromDeployment(rd)
	helmListOut, _, _ := shellout(HelmApp + " list -f \"^" + helmChartName + "$\" -n " + rd.Namespace + " | wc -l")
	helmListOut = strings.Replace(helmListOut, "\n", "", -1)
	helmListOutInt, err := strconv.Atoi(helmListOut)
	if err != nil {
		sugar.Error(err)
	} else if helmListOutInt > 1 {
		isFirstInstallDone = true
	}
	return isFirstInstallDone
}

func SetHelmChartAppVersion(groupPath string, rd *RelizaDeployment) error {
	var err error
	if len(rd.AppVersion) > 0 {
		helmChartName := GetChartNameFromDeployment(rd)
		_, _, err = shellout("sed -i \"s/^appVersion:.*$/appVersion: " + rd.AppVersion + "/\" " + groupPath + helmChartName + "/Chart.yaml")
	}
	return err
}

func InstallHelmChart(groupPath string, rd *RelizaDeployment) error {
	helmChartName := GetChartNameFromDeployment(rd)
	sugar.Info("Installing chart ", helmChartName, " for namespace ", rd.Namespace)
	helmCmd := HelmApp + " upgrade --install " + helmChartName + " --create-namespace -n " + rd.Namespace + " -f " + groupPath + InstallValues + " " + groupPath + helmChartName
	sugar.Info("Helm install command: ", helmCmd)
	sugar.Info("Using values file: ", groupPath+InstallValues)
	stdout, stderr, err := dryRunShellout(helmCmd)
	if err == nil {
		sugar.Info("Successfully deployed chart ", helmChartName, " version ", rd.ArtVersion, " to namespace ", rd.Namespace)
	} else {
		sugar.Error("Failed to install chart: ", err)
		sugar.Error("stdout: ", stdout)
		sugar.Error("stderr: ", stderr)
	}
	return err
}

func RecordDeployedData(groupPath string, rd *RelizaDeployment) {
	rdJson, err := json.Marshal(rd)
	if err != nil {
		sugar.Error(err)
	}
	rdFile, err := os.Create(groupPath + RecordedDeloyedData)
	if err != nil {
		sugar.Error(err)
	}
	rdFile.Write(rdJson)
	rdFile.Close()
}

func RecordHelmChartVersion(groupPath string, rd *RelizaDeployment) {
	shellout("echo " + rd.ArtVersion + " > " + groupPath + LastVersionFile)
}

func GetLastHelmVersion(groupPath string) string {
	lastVerOut, _, _ := shellout("cat " + groupPath + LastVersionFile + " || echo -n 'none'")
	lastVerOut = strings.Replace(lastVerOut, "\n", "", -1)
	return lastVerOut
}

func sortPathsPerNamespace(ppn *PathsPerNamespace) []string {
	sortedPaths := ppn.Paths
	if len(sortedPaths) > 1 {
		sort.Slice(sortedPaths, func(i, j int) bool {
			return sortedPaths[i] < sortedPaths[j]
		})
	}
	return sortedPaths
}

func StreamHelmChartMetadataToHub(ppn *PathsPerNamespace) {
	images := ""
	sortedPaths := sortPathsPerNamespace(ppn)
	for _, groupPath := range sortedPaths {
		images += " " + getHelmChartDigest(groupPath)
	}

	doStream := true

	if len(images) < 1 {
		images = " " // required otherwise cli looks for images in file
	} else {
		doStream = isHelmWatcherStreamDiff(ppn, images)
	}

	sugar.Debug("Helm images = ", images, " , doStream = ", doStream)

	if doStream {
		sendMetaCmd := RelizaCliApp + " instdata --images \"" + images + "\" --namespace " + ppn.Namespace + " --sender helmsender" + ppn.Namespace
		sugar.Info(sendMetaCmd)
		_, _, err := shellout(RelizaCliApp + " instdata --images \"" + images + "\" --namespace " + ppn.Namespace + " --sender helmsender" + ppn.Namespace)
		if err == nil {
			recordStreamedHelmData(ppn, images)
		}
	}
}

func isHelmWatcherStreamDiff(ppn *PathsPerNamespace, curImages string) bool {
	isDiff := false
	prevStream, err := os.ReadFile("workspace/" + ppn.Namespace + WatcherHelmDataSuffix)
	if err != nil && os.IsNotExist(err) {
		isDiff = true
	} else if err != nil {
		sugar.Error(err)
		isDiff = true
	}

	if !isDiff {
		var prevNsi NamespaceImages
		json.Unmarshal(prevStream, &prevNsi)

		if 0 != strings.Compare(curImages, prevNsi.Images) {
			isDiff = true
		}
	}
	return isDiff
}

func getHelmChartDigest(groupPath string) string {
	digest, _, _ := shellout("sha256sum " + groupPath + "*.tgz | cut -f 1 -d ' '")
	digest = strings.ReplaceAll(digest, "\n", "")
	return "sha256:" + digest
}

func recordStreamedHelmData(ppn *PathsPerNamespace, images string) {
	var nsImages NamespaceImages
	nsImages.Namespace = ppn.Namespace
	nsImages.Images = images

	nsiJson, err := json.Marshal(nsImages)
	if err != nil {
		sugar.Error(err)
	}
	nsiFile, err := os.Create("workspace/" + ppn.Namespace + WatcherHelmDataSuffix)
	if err != nil {
		sugar.Error(err)
	}
	nsiFile.Write(nsiJson)
	nsiFile.Close()
}

func GetChartNameFromDeployment(rd *RelizaDeployment) string {
	helmChartSplit := strings.Split(rd.ArtUri, "/")
	return helmChartSplit[len(helmChartSplit)-1]
}

func CreateNamespaceIfMissing(namespace string) {
	nsListOut, _, _ := shellout(KubectlApp + " get ns " + namespace + " | wc -l")
	nsListOut = strings.Replace(nsListOut, "\n", "", -1)
	nsListOutInt, err := strconv.Atoi(nsListOut)
	if err != nil {
		sugar.Error(err)
	} else if nsListOutInt < 2 {
		dryRunShellout(KubectlApp + " create ns " + namespace)
	}
}

func DeleteObsoleteDeployment(groupPath string) {
	recordedDataPath := groupPath + RecordedDeloyedData

	// Check if recorded deployment data file exists
	if _, err := os.Stat(recordedDataPath); os.IsNotExist(err) {
		sugar.Warnw("Recorded deployment data not found for obsolete deployment, skipping cleanup",
			"path", recordedDataPath)
		// Still remove the directory to clean up
		os.RemoveAll(groupPath)
		return
	}

	recordedData, err := os.ReadFile(recordedDataPath)
	if err != nil {
		sugar.Error(err)
	} else {
		var rd RelizaDeployment
		json.Unmarshal(recordedData, &rd)
		helmChartName := GetChartNameFromDeployment(&rd)
		if !argoInfo.IsArgoEnabled {
			sugar.Info("Uninstalling chart ", helmChartName, " from namespace ", rd.Namespace)
			dryRunShellout(HelmApp + " uninstall " + helmChartName + " -n " + rd.Namespace)
		} else {
			sugar.Info("Uninstalling argo application for release", rd.Name, " from namespace ", rd.Namespace)
			dryRunShellout(KubectlApp + " delete application -l 'reliza.io/type=cdresource' -l 'reliza.io/name=" + rd.Name + "' -n " + SecretsNamespace)
		}

		dryRunShellout(KubectlApp + " delete sealedsecret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=" + rd.Name + "' -n " + SecretsNamespace)
		dryRunShellout(KubectlApp + " delete sealedsecret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=ecr-" + rd.Name + "' -n " + SecretsNamespace)
		dryRunShellout(KubectlApp + " delete secret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=" + rd.Name + "' -n " + SecretsNamespace)
		os.RemoveAll(groupPath)
	}
}

type PathsPerNamespace struct {
	Namespace string
	Paths     []string
	isEmpty   bool
}

type NamespaceImages struct {
	Namespace string
	Images    string
}

type SecretPropsCliResponse struct {
	Secrets    []ResolvedSecret   `json:"secrets"`
	Properties []ResolvedProperty `json:"properties"`
}

type ResolvedSecret struct {
	Secret    string `json:"value"`
	Timestamp int64  `json:"lastUpdated"`
	Key       string `json:"key"`
}

type ResolvedProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

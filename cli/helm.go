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
	"strconv"
	"strings"
)

const (
	HelmApp             = "tools/helm"
	KubectlApp          = "tools/kubectl"
	WorkValues          = "work-values.yaml"
	ValuesDiff          = "values-diff.yaml"
	ValuesDiffPrev      = "values-diff-prev.yaml"
	LastVersionFile     = "last_version"
	InstallValues       = "install-values.yaml"
	RecordedDeloyedData = "recorded-deployed-data.json"
)

func InstallSealedCertificates() {
	sugar.Info("Installing Bitnami Sealed Certificate")
	// https://github.com/bitnami-labs/sealed-secrets#helm-chart
	shellout(HelmApp + " repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets")
	shellout(HelmApp + " install sealed-secrets -n kube-system --set-string fullnameOverride=sealed-secrets-controller sealed-secrets/sealed-secrets")
}

func ResolveHelmAuthSecret(secretName string) ProjectAuth {
	var pa ProjectAuth
	username, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.username} -n " + MyNamespace + " | base64 -d")
	password, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.password} -n " + MyNamespace + " | base64 -d")
	url, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.url} -n " + MyNamespace + " | base64 -d")
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
	shellout(KubectlApp + " apply -f " + path)
}

func cleanupHelmChart(helmChartPath string) {
	shellout("rm -rf " + helmChartPath + "/")
	shellout("rm -rf " + helmChartPath + "*.tgz")
}

func DownloadHelmChart(path string, rd *RelizaDeployment, pa *ProjectAuth) error {
	var err error
	helmChartName := getChartNameFromDeployment(rd)
	helmChartUri := strings.Replace(rd.ArtUri, "/"+helmChartName, "", -1)

	cleanupHelmChart(path + helmChartName)

	// TODO flag for OCI from RH
	useOci := false
	if strings.Contains(rd.ArtUri, "azurecr.io") || strings.Contains(rd.ArtUri, ".ecr.") || strings.Contains(rd.ArtUri, ".pkg.dev") {
		useOci = true
	}
	if useOci {
		ociUri := strings.Replace(rd.ArtUri, "https://", "", -1)
		ociUri = strings.Replace(ociUri, "http://", "", -1)
		ociUri = "oci://" + ociUri
		if pa.Type != "NOCREDS" {
			_, _, err = shellout(HelmApp + " registry login " + helmChartUri + " --username " + pa.Login + " --password " + pa.Password)
		} else {
			_, _, err = shellout(HelmApp + " registry login " + helmChartUri)
		}
		if err == nil {
			_, _, err = shellout(HelmApp + " pull " + ociUri + " --version " + rd.ArtVersion + " -d " + path)
		}
	} else {
		if pa.Type != "NOCREDS" {
			_, _, err = shellout(HelmApp + " repo add " + helmChartName + " " + helmChartUri + " --username " + pa.Login + " --password " + pa.Password)
		} else {
			_, _, err = shellout(HelmApp + " repo add " + helmChartName + " " + helmChartUri)
		}
		if err == nil {
			shellout(HelmApp + " repo update " + helmChartName)
			shellout(HelmApp + " pull " + helmChartName + "/" + helmChartName + " --version " + rd.ArtVersion + " -d " + path)
		}
	}
	if err == nil {
		_, _, err = shellout("tar -xzvf " + path + "*.tgz -C " + path)
	}
	return err
}

func MergeHelmValues(groupPath string, rd *RelizaDeployment) {
	helmChartName := getChartNameFromDeployment(rd)
	helmValuesCmd := RelizaCliApp + " helmvalues " + groupPath + helmChartName + " -f " + rd.ConfigFile + " --outfile " + groupPath + WorkValues
	shellout(helmValuesCmd)
}

func ResolvePreviousDiffFile(groupPath string) {
	os.RemoveAll(groupPath + ValuesDiffPrev)
	shellout("cp " + groupPath + ValuesDiff + " " + groupPath + ValuesDiffPrev +
		" || echo 'no prev values file present yet' > " + groupPath + ValuesDiffPrev)
}

func ReplaceTagsForDiff(groupPath string, namespace string) {
	replaceTagsCmd := RelizaCliApp + " replacetags --infile " + groupPath + WorkValues + " --outfile " + groupPath + ValuesDiff + " --fordiff=true --resolveprops=true --namespace " + namespace
	shellout(replaceTagsCmd)
}

func ReplaceTagsForInstall(groupPath string, namespace string) {
	replaceTagsCmd := RelizaCliApp + " replacetags --infile " + groupPath + WorkValues + " --outfile " + groupPath + InstallValues + " --resolveprops=true --namespace " + namespace
	shellout(replaceTagsCmd)
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

func IsFirstInstallDone(rd *RelizaDeployment) bool {
	isFirstInstallDone := false
	helmChartName := getChartNameFromDeployment(rd)
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

func SetHelmChartAppVersion(groupPath string, rd *RelizaDeployment) {
	if len(rd.AppVersion) > 0 {
		helmChartName := getChartNameFromDeployment(rd)
		shellout("sed -i \"s/^appVersion:.*$/appVersion: " + rd.AppVersion + "/\" " + groupPath + helmChartName + "/Chart.yaml")
	}
}

func InstallHelmChart(groupPath string, rd *RelizaDeployment) error {
	helmChartName := getChartNameFromDeployment(rd)
	sugar.Info("Installing chart ", helmChartName, " for namespace ", rd.Namespace)
	_, _, err := shellout(HelmApp + " upgrade --install " + helmChartName + " --create-namespace -n " + rd.Namespace + " -f " + groupPath + InstallValues + " " + groupPath + helmChartName)
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

func StreamHelmChartsMetadataToHub(nsGroupPaths *[]string, namespace string) {
	images := ""
	for _, groupPath := range *nsGroupPaths {
		images += " " + getHelmChartDigest(groupPath)
	}
	if len(images) > 1 {
		shellout(RelizaCliApp + " instdata --images \"" + images + "\" --namespace " + namespace + " --sender helmsender" + namespace)
	}
}

func getHelmChartDigest(groupPath string) string {
	digest, _, _ := shellout("sha256sum " + groupPath + "*.tgz | cut -f 1 -d ' '")
	return "sha256:" + digest
}

func getChartNameFromDeployment(rd *RelizaDeployment) string {
	helmChartSplit := strings.Split(rd.ArtUri, "/")
	return helmChartSplit[len(helmChartSplit)-1]
}

func DeleteObsoleteDeployment(groupPath string) {
	recordedData, err := os.ReadFile(groupPath + RecordedDeloyedData)
	if err != nil {
		sugar.Error(err)
	} else {
		var rd RelizaDeployment
		json.Unmarshal(recordedData, &rd)
		helmChartName := getChartNameFromDeployment(&rd)
		sugar.Info("Uninstalling chart ", helmChartName, " from namespace ", rd.Namespace)
		shellout(HelmApp + " uninstall " + helmChartName + " -n " + rd.Namespace)
		shellout(KubectlApp + " delete sealedsecret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=" + rd.Name + "' -n " + MyNamespace)
		shellout(KubectlApp + " delete sealedsecret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=ecr-" + rd.Name + "' -n " + MyNamespace)
		shellout(KubectlApp + " delete secret -l 'reliza.io/type=cdresource' -l 'reliza.io/name=" + rd.Name + "' -n " + MyNamespace)
		os.RemoveAll(groupPath)
	}
}

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
	"os"
	"strings"
)

const (
	HelmApp        = "tools/helm"
	KubectlApp     = "tools/kubectl"
	MyNamespace    = "argocd" // TODO make configurable
	WorkValues     = "work-values.yaml"
	ValuesDiff     = "values-diff.yaml"
	ValuesDiffPrev = "values-diff-prev.yaml"
)

func InstallSealedCertificates() {
	sugar.Info("Installing Bitnami Sealed Certificate")
	// https://github.com/bitnami-labs/sealed-secrets#helm-chart
	shellout(HelmApp + " repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets")
	shellout(HelmApp + " install sealed-secrets -n kube-system --set-string fullnameOverride=sealed-secrets-controller sealed-secrets/sealed-secrets")
}

func ResolveHelmAuthSecret(secretName string) ProjectAuth {
	var pa ProjectAuth
	username, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.username} -n" + MyNamespace + " | base64 -d")
	password, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.password} -n" + MyNamespace + " | base64 -d")
	pa.Type = "CREDS"
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

func DownloadHelmChart(path string, rd *RelizaDeployment, pa *ProjectAuth) {
	helmChartSplit := strings.Split(rd.ArtUri, "/")
	helmChartName := helmChartSplit[len(helmChartSplit)-1]
	helmChartUri := strings.Replace(rd.ArtUri, "/"+helmChartName, "", -1)

	cleanupHelmChart(path + helmChartName)

	// TODO flag for OCI from RH
	useOci := false
	if strings.Contains(rd.ArtUri, "azurecr.io") || strings.Contains(rd.ArtUri, ".ecr.") || strings.Contains(rd.ArtUri, ".pkg.dev") {
		useOci = true
	}
	if useOci {
		// TODO: test oci
		// TODO: special case for ECR
		sugar.Info(helmChartUri)
		ociUri := strings.Replace(rd.ArtUri, "https://", "oci://", -1)
		ociUri = strings.Replace(ociUri, "http://", "oci://", -1)
		shellout(HelmApp + " registry login " + helmChartUri + " --username " + pa.Login + " --password " + pa.Password)
		pullCmd := HelmApp + " pull " + ociUri + " --username " + pa.Login + " --password " + pa.Password + " --version " + rd.ArtVersion + " -d " + path
		shellout(pullCmd)
	} else {
		shellout(HelmApp + " repo add " + helmChartName + " " + helmChartUri + " --username " + pa.Login + " --password " + pa.Password)
		shellout(HelmApp + " repo update")
		shellout(HelmApp + " pull " + helmChartName + "/" + helmChartName + " --version " + rd.ArtVersion + " -d " + path)
	}

	shellout("tar -xzvf " + path + "*.tgz -C " + path)
}

func MergeHelmValues(groupPath string, rd *RelizaDeployment) {
	helmChartSplit := strings.Split(rd.ArtUri, "/")
	helmChartName := helmChartSplit[len(helmChartSplit)-1]
	helmValuesCmd := RelizaCliApp + " helmvalues " + groupPath + helmChartName + " -f " + rd.ConfigFile + " --outfile " + groupPath + WorkValues
	shellout(helmValuesCmd)
}

func ResolvePreviousDiffFile(groupPath string) {
	os.RemoveAll(groupPath + ValuesDiffPrev)
	shellout("cp " + groupPath + ValuesDiff + " " + groupPath + ValuesDiffPrev +
		" || echo 'no prev values file present yet' > " + groupPath + ValuesDiffPrev)
}

func ReplaceTags(groupPath string, namespace string) {
	replaceTagsCmd := RelizaCliApp + " replacetags --infile " + groupPath + WorkValues + " --outfile " + groupPath + ValuesDiff + " --fordiff=true --resolveprops=true --namespace " + namespace
	shellout(replaceTagsCmd)
}

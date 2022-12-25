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

import "strings"

const (
	HelmApp     = "tools/helm"
	KubectlApp  = "tools/kubectl"
	MyNamespace = "argocd" // TODO make configurable
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

func downloadChart(path string, rd *RelizaDeployment, pa *ProjectAuth) {
	// TODO flag for OCI from RH
	useOci := false
	if strings.Contains(rd.ArtUri, "azurecr.io") || strings.Contains(rd.ArtUri, ".ecr.") {
		useOci = true
	}
	if useOci {

	}

}
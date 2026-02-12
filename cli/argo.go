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
	"io"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/relizaio/reliza-cd/utils"
)

type ArgoApplicationTemplateResolver struct {
	Name                string
	ArgoNamespace       string
	ReleaseNamespace    string
	HelmChartName       string
	MegedValuesFromFile string
	ChartUri            string
	Version             string
}
type ArgoInfo struct {
	IsArgoDetected bool
	IsArgoEnabled  bool
	ArgoNamespace  string
}

func detectArgo() ArgoInfo {
	var argoInfo ArgoInfo
	argoInfo.IsArgoDetected = false
	argoInfo.IsArgoEnabled = false

	if EnvMode != StandaloneMode {
		argoInfo.IsArgoEnabled = true
	}

	if argoInfo.IsArgoEnabled {
		retryLeft := 3
		for !argoInfo.IsArgoDetected && retryLeft > 0 {
			argoDetectedOut, _, _ := shellout(KubectlApp + " get pods -A | grep argocd | wc -l")
			argoDetectedOut = strings.Replace(argoDetectedOut, "\n", "", -1)
			argoDetectedOutInt, err := strconv.Atoi(argoDetectedOut)

			if err != nil {
				sugar.Error(err)
			} else if argoDetectedOutInt > 0 {
				argoInfo.IsArgoDetected = true
			} else {
				retryLeft--
				sugar.Warn("Could not detect argocd, retries left = ", retryLeft)
				time.Sleep(2 * time.Second)
			}
		}

		argoInfo.ArgoNamespace, _, _ = shellout(KubectlApp + " get secrets -A | grep argocd-initial-admin-secret | awk '{ print $1 }'")
		argoInfo.ArgoNamespace = strings.Replace(argoInfo.ArgoNamespace, "\n", "", -1)

	}

	return argoInfo
}

func IsFirstArgoInstallDone(rd *RelizaDeployment) bool {
	isFirstInstallDone := false
	argoAppListOut, _, _ := shellout(KubectlApp + " kubectl get applications -A | grep " + rd.Name + " | wc -l")
	argoAppListOut = strings.Replace(argoAppListOut, "\n", "", -1)
	argoAppListOutInt, err := strconv.Atoi(argoAppListOut)

	if err != nil {
		sugar.Error(err)
	} else if argoAppListOutInt > 0 {
		isFirstInstallDone = true
	}
	return isFirstInstallDone
}

func indent(s string, n int) string {
	s = strings.TrimSpace(s)
	indentStr := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = indentStr + line
	}
	return strings.Join(lines, "\n")
}

func ProduceArgoApplicationYaml(w io.Writer, rd *RelizaDeployment, namespace, groupPath string) error {
	applicationTmpl :=
		`apiVersion: argoproj.io/v1alpha1	
kind: Application
metadata:
  name: {{.Name}}
  namespace: {{.ArgoNamespace}}
  finalizers:
    - resources-finalizer.argocd.argoproj.io
  labels:
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
spec:
  syncPolicy:
    automated: {}
  destination:
    namespace: {{.ReleaseNamespace}}
    server: https://kubernetes.default.svc
  project: default
  source:
    chart: {{.HelmChartName}}
    helm:
      values: |
{{ indent .MegedValuesFromFile 8 }}
    repoURL: {{.ChartUri}}
    targetRevision: {{.Version}}`

	helmRepoInfo := GetHelmRepoInfoFromDeployment(rd)

	helmValues, err := os.ReadFile(groupPath + InstallValues)
	if err != nil {
		sugar.Error(err)
	}

	argoAppTmplRes := ArgoApplicationTemplateResolver{
		Name:                rd.Name,
		ArgoNamespace:       namespace,
		ReleaseNamespace:    rd.Namespace,
		HelmChartName:       helmRepoInfo.ChartName,
		ChartUri:            helmRepoInfo.RepoHost,
		Version:             rd.ArtVersion,
		MegedValuesFromFile: string(helmValues),
	}
	if !helmRepoInfo.UseOci {
		argoAppTmplRes.ChartUri = helmRepoInfo.RepoUri
	}
	funcMap := template.FuncMap{
		"indent": indent,
	}
	tmpl, err := template.New("applicationtmpl").Funcs(funcMap).Parse(applicationTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, argoAppTmplRes)
	if err != nil {
		panic(err)
	}
	return err
}

func installArgoApplication(groupPath string, rd *RelizaDeployment, argoNameSpace string) error {

	applicationPath := groupPath + "argo-app.yaml"
	applicationFile := utils.CreateFile(applicationPath)
	err := ProduceArgoApplicationYaml(applicationFile, rd, SecretsNamespace, groupPath)

	if err != nil {
		return err
	}
	CreateNamespaceIfMissing(rd.Namespace)
	KubectlApply(applicationPath)
	return nil
}

func installArgoCD() {
	sugar.Info("Installing argocd")
	dryRunShellout(HelmApp + " repo add argo https://argoproj.github.io/argo-helm")
	dryRunShellout(HelmApp + " repo update")
	retryLeft := 3
	argocdInstalled := false
	argoVersion := os.Getenv("ARGO_HELM_VERSION")
	for !argocdInstalled && retryLeft > 0 {
		_, _, err := dryRunShellout(HelmApp + " upgrade --install --create-namespace --set dex.enabled=false --set notifications.enabled=false --set applicationSet.enabled=false --set configs.params.server.insecure=true -n argocd argocd argo/argo-cd --version " + argoVersion)
		if err == nil {
			argocdInstalled = true
		} else {
			retryLeft--
			sugar.Warn("Could not install argocd, retries left = ", retryLeft)
			time.Sleep(2 * time.Second)
		}
	}
	sugar.Info("Waiting for argocd installation to complete ...")
	dryRunShellout("while ! " + KubectlApp + " get secrets -A | grep argocd-initial-admin-secret; do sleep 1; done")
	sugar.Info("argocd installation complete.")

}

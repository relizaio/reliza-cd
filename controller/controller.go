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
package controller

import (
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/relizaio/reliza-cd/cli"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var sugar *zap.SugaredLogger

func init() {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	var logger, _ = config.Build()
	defer logger.Sync()
	sugar = logger.Sugar()
}

func loopInit() {
	sealedCert := cli.GetSealedCert()
	if len(sealedCert) < 1 {
		cli.InstallSealedCertificates()
		for len(sealedCert) < 1 {
			sealedCert = cli.GetSealedCert()
			time.Sleep(3 * time.Second)
		}
		sugar.Info("Installed Bitnami Sealed Certificates")
	}

	cli.SetSealedCertificateOnTheHub(sealedCert)
}

func singleLoopRun() {
	instManifest, err := cli.GetInstanceCycloneDX()

	if err == nil {
		rlzDeployments := cli.ParseInstanceCycloneDXIntoDeployments(instManifest)

		existingDeployments := collectExistingDeployments()

		namespacesForWatcher := make(map[string]bool)

		for _, rd := range rlzDeployments {
			existingDeployments[rd.Name] = true
			processSingleDeployment(&rd)
			namespacesForWatcher[rd.Namespace] = true
		}

		cli.InstallWatcher(&namespacesForWatcher)

		deleteObsoleteDeployments(&existingDeployments)

		helmDataStreamToHub(&existingDeployments)
	}
}

func Loop() {
	loopInit()

	for true {
		singleLoopRun()
		time.Sleep(15 * time.Second)
	}
}

func helmDataStreamToHub(existingDeployments *map[string]bool) {
	// collect per namespace
	perNamespaceActiveDepl := map[string]cli.PathsPerNamespace{}
	for edKey, edVal := range *existingDeployments {
		ns := getNamespaceFromPath(edKey)
		curPaths, exists := perNamespaceActiveDepl[ns]
		if exists && edVal {
			curPaths.Paths = append(curPaths.Paths, "workspace/"+edKey+"/")
		} else if edVal {
			curPaths = cli.PathsPerNamespace{}
			curPaths.Paths = append(curPaths.Paths, "workspace/"+edKey+"/")
			curPaths.Namespace = ns
			perNamespaceActiveDepl[ns] = curPaths
		} else if !exists {
			curPaths = cli.PathsPerNamespace{}
			curPaths.Paths = []string{}
			curPaths.Namespace = ns
			perNamespaceActiveDepl[ns] = curPaths
		}
	}

	for _, ppn := range perNamespaceActiveDepl {
		cli.StreamHelmChartMetadataToHub(&ppn)
	}

}

func getNamespaceFromPath(path string) string {
	return strings.Split(path, "---")[0]
}

func deleteObsoleteDeployments(existingDeployments *map[string]bool) {
	for edKey, edVal := range *existingDeployments {
		if !edVal {
			cli.DeleteObsoleteDeployment("workspace/" + edKey + "/")
		}
	}
}

func collectExistingDeployments() map[string]bool {
	existingDeployments := make(map[string]bool)
	workspaceEntries, err := ioutil.ReadDir("workspace")
	if err != nil {
		sugar.Error(err)
	}
	for _, we := range workspaceEntries {
		if we.IsDir() && we.Name() != "watcher" {
			existingDeployments[we.Name()] = false
		}
	}
	return existingDeployments
}

func createSecretFile(filePath string) *os.File {
	ecrSecretFile, err := os.Create(filePath)
	if err != nil {
		sugar.Error(err)
	}
	return ecrSecretFile
}

func processSingleDeployment(rd *cli.RelizaDeployment) {
	digest := cli.ExtractRlzDigestFromCdxDigest(rd.ArtHash)
	projAuth := cli.GetProjectAuthByArtifactDigest(digest)
	dirName := rd.Name
	os.MkdirAll("workspace/"+dirName, 0700)
	groupPath := "workspace/" + dirName + "/"

	var helmDownloadPa cli.ProjectAuth

	doInstall := false
	isError := false

	helmDownloadPa.Type = projAuth.Type

	if projAuth.Type == "ECR" {
		ecrSecretPath := "workspace/" + dirName + "/ecrreposecret.yaml"
		ecrSecretFile := createSecretFile(ecrSecretPath)
		cli.ProduceEcrSecretYaml(ecrSecretFile, rd, projAuth, cli.MyNamespace)
		cli.KubectlApply(ecrSecretPath)
		ecrAuthPa := cli.ResolveHelmAuthSecret("ecr-" + dirName)
		ecrToken := getEcrToken(&ecrAuthPa)
		var paForPlainSecret cli.ProjectAuth
		paForPlainSecret.Login = "AWS"
		paForPlainSecret.Password = ecrToken
		paForPlainSecret.Type = "ECR"
		paForPlainSecret.Url = ecrAuthPa.Url
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile := createSecretFile(secretPath)
		cli.ProducePlainSecretYaml(secretFile, rd, paForPlainSecret, cli.MyNamespace)
		cli.KubectlApply(secretPath)
		helmDownloadPa = cli.ResolveHelmAuthSecret(dirName)
	}

	if projAuth.Type == "CREDS" {
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile := createSecretFile(secretPath)
		cli.ProduceSecretYaml(secretFile, rd, projAuth, cli.MyNamespace)
		cli.KubectlApply(secretPath)
		helmDownloadPa = cli.ResolveHelmAuthSecret(dirName)
	}

	var err error
	lastHelmVer := cli.GetLastHelmVersion(groupPath)
	if rd.ArtVersion != lastHelmVer {
		err = cli.DownloadHelmChart(groupPath, rd, &helmDownloadPa)
		if err == nil {
			cli.RecordHelmChartVersion(groupPath, rd)
			doInstall = true
		} else {
			isError = true
		}
	}

	if !isError {
		err = cli.ResolvePreviousDiffFile(groupPath)
		isError = (err != nil)
	}

	if !isError {
		err = cli.MergeHelmValues(groupPath, rd)
		isError = (err != nil)
	}

	if !isError {
		err = cli.ReplaceTagsForDiff(groupPath, rd.Namespace)
		isError = (err != nil)
	}

	if !isError && !doInstall {
		doInstall = cli.IsValuesDiff(groupPath)
	}
	if !isError && !doInstall {
		doInstall = !cli.IsFirstInstallDone(rd)
	}

	if !isError && doInstall {
		err = cli.SetHelmChartAppVersion(groupPath, rd)
		isError = (err != nil)
	}

	if !isError && doInstall {
		err = cli.ReplaceTagsForInstall(groupPath, rd.Namespace)
		isError = (err != nil)
	}

	if !isError && doInstall {
		err := cli.InstallHelmChart(groupPath, rd)
		isError = (err != nil)
	}

	if !isError && doInstall {
		cli.RecordDeployedData(groupPath, rd)
	}
}

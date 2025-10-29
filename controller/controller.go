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
	"github.com/relizaio/reliza-cd/utils"
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
	sugar.Info("Starting loopInit - getting sealed cert")
	sealedCert := cli.GetSealedCert()
	sugar.Info("Got sealed cert, length: ", len(sealedCert))
	if len(sealedCert) < 1 {
		sugar.Info("Sealed cert is empty, installing sealed certificates")
		cli.InstallSealedCertificates()
		for len(sealedCert) < 1 {
			sealedCert = cli.GetSealedCert()
			time.Sleep(3 * time.Second)
		}
		sugar.Info("Installed Bitnami Sealed Certificates")
	}

	sugar.Info("Setting sealed certificate on the hub")
	cli.SetSealedCertificateOnTheHub(sealedCert)
	sugar.Info("Completed loopInit")
}

func singleLoopRun() {
	instManifest, err := cli.GetInstanceCycloneDX()

	if err != nil {
		sugar.Error(err)
	}

	if err == nil {
		rlzDeployments := cli.ParseInstanceCycloneDXIntoDeployments(instManifest)

		existingDeployments := collectExistingDeployments()

		namespacesForWatcher := make(map[string]bool)

		isError := false

		for _, rd := range rlzDeployments {
			existingDeployments[rd.Name] = true
			err = processSingleDeployment(&rd)
			if err != nil {
				// Errors already logged in processSingleDeployment with full context
				sugar.Infow("Skipping deployment due to error",
					"bundle", rd.Bundle,
					"version", rd.ArtVersion,
					"namespace", rd.Namespace,
					"deploymentName", rd.Name)
			}
			isError = (err != nil)
			namespacesForWatcher[rd.Namespace] = true
			cli.CreateNamespaceIfMissing(rd.Namespace)
		}

		cli.InstallWatcher(&namespacesForWatcher)

		if !isError {
			deleteObsoleteDeployments(&existingDeployments)
		}

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
			perNamespaceActiveDepl[ns] = curPaths
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
		if we.IsDir() && we.Name() != "watcher" && we.Name() != "lost+found" {
			existingDeployments[we.Name()] = false
		}
	}
	return existingDeployments
}

func processSingleDeployment(rd *cli.RelizaDeployment) error {
	if cli.SecretsNamespace == "" {
		sugar.Info("SecretNS is null")
		panic("secretnamespace must be set by this point")
	}
	var projAuth cli.ProjectAuth
	if rd.ArtHash.Value == "" {
		// No hash means public repo, assume NOCREDS
		projAuth.Type = "NOCREDS"
	} else {
		digest := cli.ExtractRlzDigestFromCdxDigest(rd.ArtHash)
		projAuth = cli.GetProjectAuthByArtifactDigest(digest, rd.Namespace)
	}
	dirName := rd.Name
	os.MkdirAll("workspace/"+dirName, 0700)
	groupPath := "workspace/" + dirName + "/"

	var helmDownloadPa cli.ProjectAuth

	doInstall := false
	isError := false
	helmDownloadPa.Type = projAuth.Type
	helmInfo := cli.GetHelmRepoInfoFromDeployment(rd)
	if projAuth.Type == "ECR" {
		ecrSecretPath := "workspace/" + dirName + "/ecrreposecret.yaml"
		ecrSecretFile := utils.CreateFile(ecrSecretPath)
		cli.ProduceEcrSecretYaml(ecrSecretFile, rd, projAuth, cli.SecretsNamespace)
		cli.KubectlApply(ecrSecretPath)
		cli.WaitUntilSecretCreated("ecr-"+rd.Name, cli.SecretsNamespace)
		ecrAuthPa := cli.ResolveHelmAuthSecret("ecr-" + dirName)
		ecrToken := getEcrToken(&ecrAuthPa)
		var paForPlainSecret cli.ProjectAuth
		paForPlainSecret.Login = "AWS"
		paForPlainSecret.Password = ecrToken
		paForPlainSecret.Type = "ECR"
		paForPlainSecret.Url = ecrAuthPa.Url
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile := utils.CreateFile(secretPath)
		cli.ProducePlainSecretYaml(secretFile, rd, paForPlainSecret, cli.SecretsNamespace, helmInfo)
		cli.KubectlApply(secretPath)
		cli.WaitUntilSecretCreated(rd.Name, cli.SecretsNamespace)
		helmDownloadPa = cli.ResolveHelmAuthSecret(dirName)
	}

	if projAuth.Type == "CREDS" {
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile := utils.CreateFile(secretPath)
		cli.ProduceSecretYaml(secretFile, rd, projAuth, cli.SecretsNamespace, helmInfo)
		cli.KubectlApply(secretPath)
		cli.WaitUntilSecretCreated(rd.Name, cli.SecretsNamespace)
		helmDownloadPa = cli.ResolveHelmAuthSecret(dirName)
	}

	if projAuth.Type == "NOCREDS" {
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile := utils.CreateFile(secretPath)
		cli.ProduceSecretYaml(secretFile, rd, projAuth, cli.SecretsNamespace, helmInfo)
		cli.KubectlApply(secretPath)
		cli.WaitUntilSecretCreated(rd.Name, cli.SecretsNamespace)
		helmDownloadPa.Url = rd.ArtUri
	}
	var err error
	lastHelmVer := cli.GetLastHelmVersion(groupPath)
	doDownloadChart := false
	if rd.ArtVersion != lastHelmVer {
		doDownloadChart = true
	} else {
		if _, err := os.Stat(groupPath + cli.GetChartNameFromDeployment(rd) + "/Chart.yaml"); err != nil {
			doDownloadChart = true
		}
	}
	if doDownloadChart {
		err = cli.DownloadHelmChart(groupPath, rd, &helmDownloadPa, helmInfo)
		if err == nil {
			cli.RecordHelmChartVersion(groupPath, rd)
			doInstall = true
		} else {
			// Error already logged in DownloadHelmChart with full context
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
		// cli.CreateNamespaceIfMissing(rd.Namespace)
		err := cli.InstallApplication(groupPath, rd)
		isError = (err != nil)
	}

	if !isError && doInstall {
		cli.RecordDeployedData(groupPath, rd)
	}

	return err
}

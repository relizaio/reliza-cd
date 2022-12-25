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
	"os"
	"strings"
	"time"

	"github.com/relizaio/reliza-cd/cli"
	"go.uber.org/zap"
)

var sugar *zap.SugaredLogger

func init() {
	var logger, _ = zap.NewProduction()
	defer logger.Sync()
	sugar = logger.Sugar()
}

func Loop() {
	sealedCert := cli.GetSealedCert()
	if len(sealedCert) < 1 {
		cli.InstallSealedCertificates()
		for len(sealedCert) < 1 {
			sealedCert = cli.GetSealedCert()
			time.Sleep(3 * time.Second)
		}
	}

	// TODO only set if changed / not set previously
	cli.SetSealedCertificateOnTheHub(sealedCert)

	instManifest := cli.GetInstanceCycloneDX()
	rlzDeployments := cli.ParseInstanceCycloneDXIntoDeployments(instManifest)

	for _, rd := range rlzDeployments {
		processSingleDeployment(&rd)
	}

	sugar.Info(rlzDeployments)
}

func processSingleDeployment(rd *cli.RelizaDeployment) {
	digest := cli.ExtractRlzDigestFromCdxDigest(rd.ArtHash)
	projAuth := cli.GetProjectAuthByArtifactDigest(digest)
	dirName := strings.ToLower(rd.Name)
	os.MkdirAll("workspace/"+dirName, 0700)

	if projAuth.Type != "NOCREDS" {
		secretPath := "workspace/" + dirName + "/reposecret.yaml"
		secretFile, err := os.Create(secretPath)
		if err != nil {
			sugar.Panic(err)
		}
		cli.ProduceSecretYaml(secretFile, rd, projAuth, "argocd")
		cli.KubectlApply(secretPath)
		resolvedPa := cli.ResolveHelmAuthSecret(dirName)
		chartPath := "workspace/" + dirName + "/"
		cli.DownloadHelmChart(chartPath, rd, &resolvedPa)
	}
}

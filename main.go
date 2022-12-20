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
package main

import (
	"bytes"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

const (
	ShellToUse   = "sh"
	HelmApp      = "tools/helm"
	KubesealApp  = "tools/kubeseal"
	RelizaCliApp = "tools/reliza-cli"
)

var sugar *zap.SugaredLogger

func init() {
	var logger, _ = zap.NewProduction()
	defer logger.Sync()
	sugar = logger.Sugar()
}

func shellout(command string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err != nil {
		sugar.Error("stderr", stderr.String(), "error", err.Error())
	}

	return stdout.String(), stderr.String(), err
}

func main() {
	sugar.Info("Starting Reliza CD")

	sealedCert := getSealedCert()
	if len(sealedCert) < 1 {
		sugar.Info("Installing Bitnami Sealed Certificate")
		installSealedCertificates()
		for len(sealedCert) < 1 {
			sealedCert = getSealedCert()
			time.Sleep(3 * time.Second)
		}
	}

	setSealedCertificateOnTheHub(sealedCert)

	sugar.Info("Done Reliza CD")
}

func getSealedCert() string {
	fetchCertArg := "--fetch-cert | base64 -w 0"
	cert, _, _ := shellout(KubesealApp + " " + fetchCertArg)
	return cert
}

func installSealedCertificates() {
	// https://github.com/bitnami-labs/sealed-secrets#helm-chart
	shellout(HelmApp + " repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets")
	shellout(HelmApp + " install sealed-secrets -n kube-system --set-string fullnameOverride=sealed-secrets-controller sealed-secrets/sealed-secrets")
}

func setSealedCertificateOnTheHub(cert string) {
	shellout(RelizaCliApp + " setsecretcert --cert " + cert)
}

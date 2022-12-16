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
	"fmt"
	"os/exec"
)

const ShellToUse = "sh"
const HelmApp = "tools/helm"
const KubesealApp = "tools/kubeseal"

func shellout(command string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func main() {
	fmt.Println("Hello world!")
	fetchCertArg := "--fetch-cert"
	out, errout, err := shellout(KubesealApp + " " + fetchCertArg)
	// cmd := exec.Command(app, fetchCertArg)

	if err != nil {
		fmt.Println(errout)
		fmt.Println(err.Error())
		return
	}

	fmt.Println(out)

	out, errout, err = shellout(HelmApp + " list")

	if err != nil {
		fmt.Println(errout)
		fmt.Println(err.Error())
		return
	}
	fmt.Println(out)
}

func initialize() {

}

func installSealedCertificates() {
	// https://github.com/bitnami-labs/sealed-secrets#helm-chart
	// helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets

}

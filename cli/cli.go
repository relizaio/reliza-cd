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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"go.uber.org/zap"
)

const (
	ShellToUse   = "sh"
	KubesealApp  = "tools/kubeseal"
	RelizaCliApp = "tools/reliza-cli"
	HelmMimeType = "application/vnd.cncf.helm.config.v1+json"
)

var (
	sugar       *zap.SugaredLogger
	MyNamespace string
)

func init() {
	var logger, _ = zap.NewProduction()
	defer logger.Sync()
	sugar = logger.Sugar()
	if len(os.Getenv("MY_NAMESPACE")) > 0 {
		MyNamespace = os.Getenv("MY_NAMESPACE")
	} else {
		MyNamespace = "argocd"
	}
}

func shellout(command string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err != nil {
		sugar.Error("stdout: ", stdout.String(), "stderr: ", stderr.String(), "error: ", err.Error())
	}

	return stdout.String(), stderr.String(), err
}

func SetSealedCertificateOnTheHub(cert string) {
	certPath := "workspace/sealedCert.pem"
	doSet := false
	existingCert, err := os.ReadFile(certPath)
	if err != nil && os.IsNotExist(err) {
		doSet = true
	} else if err != nil {
		doSet = true
		sugar.Error(err)
	} else if 0 != strings.Compare(cert, string(existingCert)) {
		doSet = true
	}

	if doSet {
		sugar.Info("Setting Bitnami Sealed Certificate on Reliza Hub")
		_, _, err := shellout(RelizaCliApp + " cd setsecretcert --cert " + cert)
		if err == nil {
			err := os.RemoveAll(certPath)
			if err != nil {
				sugar.Error(err)
			}
			certCheckFile, err := os.Create(certPath)
			if err != nil {
				sugar.Error(err)
			}
			certCheckFile.WriteString(cert)
			err = certCheckFile.Close()
			if err != nil {
				sugar.Error(err)
			}
		}
		sugar.Info("Set Bitnami Sealed Certificate on Reliza Hub")
	}
}

func GetInstanceCycloneDX() (string, error) {
	instManifest, _, err := shellout(RelizaCliApp + " exportinst")
	return instManifest, err
}

func ExtractRlzDigestFromCdxDigest(cdxHash cdx.Hash) string {
	algstr := strings.ToLower(string(cdxHash.Algorithm))
	algstr = strings.Replace(algstr, "-", "", -1)
	return algstr + ":" + cdxHash.Value
}

func GetSealedCert() string {
	fetchCertArg := "--fetch-cert | base64 -w 0"
	cert, _, _ := shellout(KubesealApp + " " + fetchCertArg)
	return cert
}

func resolveDeploymentNameFromString(origName string) string {
	rdName := strings.ToLower(origName)
	rdName = strings.ReplaceAll(rdName, " ", "-")
	return rdName
}

func produceAppConfigMapFromCdxComponents(cdxComponents *[]cdx.Component) map[string]appConfig {
	appConfigMap := make(map[string]appConfig)
	if nil != cdxComponents && len(*cdxComponents) > 0 {
		for _, comp := range *cdxComponents {
			if comp.Type == "application" {
				var appConfig appConfig
				appConfig.AppVersion = comp.Version
				if len(*comp.Properties) > 0 {
					for _, prop := range *comp.Properties {
						if prop.Name == "CONFIGURATION" && prop.Value != "default" {
							appConfig.ValuesFile = prop.Value
						} else {
							appConfig.ValuesFile = "values.yaml"
						}
					}
				}
				appConfigMap[strings.ToLower(comp.Group)] = appConfig
			}
		}
	}
	return appConfigMap
}

func ParseInstanceCycloneDXIntoDeployments(cyclonedxManifest string) []RelizaDeployment {
	bom := new(cdx.BOM)
	manifestReader := strings.NewReader(cyclonedxManifest)
	decoder := cdx.NewBOMDecoder(manifestReader, cdx.BOMFileFormatJSON)
	if err := decoder.Decode(bom); err != nil {
		sugar.Error(err)
	}

	var rlzDeployments []RelizaDeployment

	appConfigMap := produceAppConfigMapFromCdxComponents(bom.Components)

	if nil != bom.Components && len(*bom.Components) > 0 {
		for _, comp := range *bom.Components {
			if comp.MIMEType == HelmMimeType {
				var rd RelizaDeployment
				rd.Name = resolveDeploymentNameFromString(comp.Group)
				namespaceBundle := strings.Split(comp.Group, "---")
				rd.Namespace = namespaceBundle[0]
				rd.Bundle = namespaceBundle[1]
				rd.ArtUri = comp.Name
				rd.ArtVersion = comp.Version
				appConfig := appConfigMap[rd.Name]
				configFile := "values.yaml"
				if len(appConfig.ValuesFile) > 0 {
					configFile = appConfig.ValuesFile
				}
				rd.ConfigFile = configFile
				appVersion := ""
				if len(appConfig.AppVersion) > 0 {
					appVersion = appConfig.AppVersion
				}
				rd.AppVersion = appVersion
				hashes := *comp.Hashes
				if len(hashes) > 0 {
					rd.ArtHash = hashes[0]
					rlzDeployments = append(rlzDeployments, rd)
				} else {
					sugar.Error("Missing Helm artifact hash for = " + rd.ArtUri + ", skipping")
				}
			}
		}
	}

	return rlzDeployments

}

func GetProjectAuthByArtifactDigest(artDigest string) ProjectAuth {
	authResp, _, _ := shellout(RelizaCliApp + " cd artsecrets --artdigest " + artDigest)
	var projectAuth map[string]ProjectAuth
	json.Unmarshal([]byte(authResp), &projectAuth)
	return projectAuth["artifactDownloadSecrets"]
}

func ProduceSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string) {
	secretTmpl :=
		`apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  annotations:
    sealedsecrets.bitnami.com/namespace-wide: "true"
spec:
  encryptedData:
    username: {{.Username}}
    password: {{.Password}}
  template:
    data:
      url: {{.Url}}
      name: {{.Name}}
      type: helm
    metadata:
      labels:
        reliza.io/type: cdresource
        argocd.argoproj.io/secret-type: repository`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = rd.Name
	secTmplRes.Namespace = namespace
	secTmplRes.Username = projAuth.Login
	secTmplRes.Password = projAuth.Password
	secTmplRes.Url = rd.ArtUri

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func ProducePlainSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string) {
	secretTmpl :=
		`apiVersion: v1
kind: Secret
metadata:
  labels:
    argocd.argoproj.io/secret-type: repository
    reliza.io/type: cdresource
  name: {{.Name}}
  namespace: {{.Namespace}}
type: Opaque
data:
  type: aGVsbQ==
  url: {{.Url}}
  name: {{.NameBase64}}
  username: {{.Username}}
  password: {{.Password}}`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = rd.Name
	secTmplRes.NameBase64 = base64.StdEncoding.EncodeToString([]byte(rd.Name))
	secTmplRes.Namespace = namespace
	secTmplRes.Username = base64.StdEncoding.EncodeToString([]byte(projAuth.Login))
	secTmplRes.Password = base64.StdEncoding.EncodeToString([]byte(projAuth.Password))
	secTmplRes.Url = base64.StdEncoding.EncodeToString([]byte(rd.ArtUri))

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func ProduceEcrSecretYaml(w io.Writer, rd *RelizaDeployment, projAuth ProjectAuth, namespace string) {
	secretTmpl :=
		`apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  annotations:
    sealedsecrets.bitnami.com/namespace-wide: "true"
spec:
  encryptedData:
    username: {{.Username}}
    password: {{.Password}}
  template:
    data:
      url: {{.Url}}
    metadata:
      labels:
        reliza.io/type: cdresource`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = "ecr-" + rd.Name
	secTmplRes.Namespace = namespace
	secTmplRes.Username = projAuth.Login
	secTmplRes.Password = projAuth.Password
	secTmplRes.Url = rd.ArtUri

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

type SecretTemplateResolver struct {
	Name       string
	Namespace  string
	Username   string
	Password   string
	Url        string
	NameBase64 string
}

type RelizaDeployment struct {
	Name       string
	Namespace  string
	Bundle     string
	ArtUri     string
	ArtVersion string
	ArtHash    cdx.Hash
	ConfigFile string
	AppVersion string
}

type ProjectAuth struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Type     string `json:"type"`
	Url      string
}

type appConfig struct {
	ValuesFile string
	AppVersion string
}

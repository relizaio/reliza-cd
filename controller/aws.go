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
	"context"
	"encoding/base64"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/relizaio/reliza-cd/cli"
)

func getRegionFromPaUrl(pa *cli.ProjectAuth) string {
	urlParts := strings.Split(pa.Url, ".")
	return urlParts[3]
}

func getEcrToken(pa *cli.ProjectAuth) string {
	region := getRegionFromPaUrl(pa)
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(pa.Login, pa.Password, "")))
	if err != nil {
		sugar.Error(err)
	}

	client := ecr.NewFromConfig(cfg)
	var authParams ecr.GetAuthorizationTokenInput

	auth, err := client.GetAuthorizationToken(context.TODO(), &authParams)
	if err != nil {
		sugar.Error(err)
	}

	// token is in form AWS:token, all base64-d
	authToken := *auth.AuthorizationData[0].AuthorizationToken

	decodedAuthToken, err := base64.StdEncoding.DecodeString(authToken)
	if err != nil {
		sugar.Error(err)
	}

	return strings.Replace(string(decodedAuthToken), "AWS:", "", -1)
}

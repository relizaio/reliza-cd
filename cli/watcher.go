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
	"regexp"
)

func installWatcher(namespacesForWatcher *map[string]bool) {
	if nil != *namespacesForWatcher && len(*namespacesForWatcher) > 0 {
		namespacesForWatcherStr := constructNamespaceStringFromMap(namespacesForWatcher)
		shellout(HelmApp + " helm repo add reliza https://registry.relizahub.com/chartrepo/library")
		shellout(HelmApp + " helm repo update reliza")
		shellout(KubectlApp + " create secret generic reliza-watcher -n " + MyNamespace + " --from-literal=reliza-api-id=" + os.Getenv("APIKEYID") + " --from-literal=reliza-api-key=" + os.Getenv("APIKEY"))
		hubUri := os.Getenv("URI")
		if len(hubUri) < 1 {
			hubUri = "https://app.relizahub.com"
		}
		shellout(HelmApp + " install reliza-watcher -n " + MyNamespace + " --set namespace=\"" + namespacesForWatcherStr + "\" --set hubUri=" + hubUri + " reliza/reliza-watcher")
	}
}

func constructNamespaceStringFromMap(namespacesForWatcher *map[string]bool) string {
	namespacenamespacesForWatcherStr := ""
	for nskey := range *namespacesForWatcher {
		namespacenamespacesForWatcherStr += nskey + "\\,"
	}
	re := regexp.MustCompile(`\\,$`)
	nsByteArr := re.ReplaceAll([]byte(namespacenamespacesForWatcherStr), []byte(""))
	namespacenamespacesForWatcherStr = string(nsByteArr)
	return namespacenamespacesForWatcherStr
}

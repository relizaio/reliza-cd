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
	"sort"
	"strings"
	"time"
)

const (
	watcherPath                = "workspace/watcher/"
	watcherLastKnownNamespaces = watcherPath + "lastKnownNamespaces"
)

func InstallWatcher(namespacesForWatcher *map[string]bool) {
	namespacesForWatcherStr := ""
	if nil != *namespacesForWatcher && len(*namespacesForWatcher) > 0 {
		namespacesForWatcherStr = constructNamespaceStringFromMap(namespacesForWatcher)
	}

	isWatcherConfigUpdated := isWatcherConfigUpdated(namespacesForWatcherStr)

	if isWatcherConfigUpdated {
		sugar.Info("Watcher config was updated, proceeding with install")
		installWatcherRoutine(namespacesForWatcherStr)
		recordWatcherConfig(namespacesForWatcherStr)
	}

}

func recordWatcherConfig(namespacesForWatcherStr string) {
	os.MkdirAll(watcherPath, 0700)
	recFile, err := os.Create(watcherLastKnownNamespaces)
	if err != nil {
		sugar.Error(err)
	}
	recFile.Write([]byte(namespacesForWatcherStr))
	recFile.Close()
}

func isWatcherConfigUpdated(namespacesForWatcherStr string) bool {
	isDiff := false
	prevVal, err := os.ReadFile(watcherLastKnownNamespaces)
	if err != nil && os.IsNotExist(err) {
		isDiff = true
	} else if err != nil {
		sugar.Error(err)
	}

	if !isDiff {
		if 0 != strings.Compare(namespacesForWatcherStr, string(prevVal)) {
			isDiff = true
		}
	}
	return isDiff
}

func installWatcherRoutine(namespacesForWatcherStr string) {
	shellout(KubectlApp + " create secret generic reliza-watcher -n " + MyNamespace + " --from-literal=reliza-api-id=" + os.Getenv("APIKEYID") + " --from-literal=reliza-api-key=" + os.Getenv("APIKEY") + " --dry-run=client -o yaml | " + KubectlApp + " apply -f -")
	hubUri := os.Getenv("URI")
	if len(hubUri) < 1 {
		hubUri = "https://app.relizahub.com"
	}
	retryLeft := 3
	watcherInstalled := false
	for !watcherInstalled && retryLeft > 0 {
		_, _, err := shellout(HelmApp + " upgrade --install reliza-watcher -n " + MyNamespace + " --set namespace=\"" + namespacesForWatcherStr + "\" --set hubUri=" + hubUri + " --version 0.0.2 oci://registry.relizahub.com/library/reliza-watcher")
		if err == nil {
			watcherInstalled = true
		} else {
			retryLeft--
			sugar.Warn("Could not install watcher, retries left = ", retryLeft)
			time.Sleep(2 * time.Second)
		}
	}
}

func sortNamespacesForWatcher(namespacesForWatcher *map[string]bool) []string {
	var sortedNamespaces []string
	for nskey := range *namespacesForWatcher {
		sortedNamespaces = append(sortedNamespaces, nskey)
	}
	if len(sortedNamespaces) > 1 {
		sort.Slice(sortedNamespaces, func(i, j int) bool {
			return sortedNamespaces[i] < sortedNamespaces[j]
		})
	}
	return sortedNamespaces
}

func constructNamespaceStringFromMap(namespacesForWatcher *map[string]bool) string {
	sortedNamespaces := sortNamespacesForWatcher(namespacesForWatcher)
	namespacenamespacesForWatcherStr := ""
	for _, nskey := range sortedNamespaces {
		namespacenamespacesForWatcherStr += nskey + "\\,"
	}
	re := regexp.MustCompile(`\\,$`)
	nsByteArr := re.ReplaceAll([]byte(namespacenamespacesForWatcherStr), []byte(""))
	namespacenamespacesForWatcherStr = string(nsByteArr)
	return namespacenamespacesForWatcherStr
}

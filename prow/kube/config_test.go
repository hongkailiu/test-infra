/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/client-go/rest"
)

func TestMergeConfigs(t *testing.T) {
	fakeConfig := func(u string) *rest.Config { return &rest.Config{Username: u} }
	cases := []struct {
		name          string
		local         *rest.Config
		foreign       map[string]rest.Config
		current       string
		buildClusters map[string]rest.Config
		expected      map[string]rest.Config
		err           bool
	}{
		{
			name: "require at least one cluster",
			err:  true,
		},
		{
			name:  "only local cluster",
			local: fakeConfig("local"),
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("local"),
				DefaultClusterAlias: *fakeConfig("local"),
			},
		},
		{
			name: "fail buildClusters without local",
			buildClusters: map[string]rest.Config{
				DefaultClusterAlias: *fakeConfig("default"),
			},
			err: true,
		},
		{
			name:  "fail buildClusters without a default context",
			local: fakeConfig("local"),
			buildClusters: map[string]rest.Config{
				"random-context": *fakeConfig("random"),
			},
			err: true,
		},
		{
			name:  "accept local + buildCluster with default",
			local: fakeConfig("local"),
			buildClusters: map[string]rest.Config{
				DefaultClusterAlias: *fakeConfig("default"),
			},
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("local"),
				DefaultClusterAlias: *fakeConfig("default"),
			},
		},
		{
			name: "foreign without local uses current as default",
			foreign: map[string]rest.Config{
				"current-context": *fakeConfig("current"),
			},
			current: "current-context",
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("current"),
				DefaultClusterAlias: *fakeConfig("current"),
				"current-context":   *fakeConfig("current"),
			},
		},
		{
			name: "reject only foreign without a current context",
			foreign: map[string]rest.Config{
				DefaultClusterAlias: *fakeConfig("default"),
			},
			err: true,
		},
		{
			name: "accept only foreign with default",
			foreign: map[string]rest.Config{
				DefaultClusterAlias: *fakeConfig("default"),
				"random-context":    *fakeConfig("random"),
			},
			current: "random-context",
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("random"),
				DefaultClusterAlias: *fakeConfig("default"),
				"random-context":    *fakeConfig("random"),
			},
		},
		{
			name:  "accept local and foreign, using local for default",
			local: fakeConfig("local"),
			foreign: map[string]rest.Config{
				"random-context": *fakeConfig("random"),
			},
			current: "random-context",
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("local"),
				DefaultClusterAlias: *fakeConfig("local"),
				"random-context":    *fakeConfig("random"),
			},
		},
		{
			name:  "merge local, foreign, buildClusters",
			local: fakeConfig("local"),
			foreign: map[string]rest.Config{
				"random-context": *fakeConfig("random"),
			},
			current: "random-context",
			buildClusters: map[string]rest.Config{
				DefaultClusterAlias: *fakeConfig("default"),
				"other-build":       *fakeConfig("other-build"),
			},
			expected: map[string]rest.Config{
				InClusterContext:    *fakeConfig("local"),
				DefaultClusterAlias: *fakeConfig("default"),
				"random-context":    *fakeConfig("random"),
				"other-build":       *fakeConfig("other-build"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := mergeConfigs(tc.local, tc.foreign, tc.current, tc.buildClusters)
			switch {
			case err != nil:
				if !tc.err {
					t.Errorf("unexpected error: %v", err)
				}
			case tc.err:
				t.Error("failed to receive an error")
			case !equality.Semantic.DeepEqual(actual, tc.expected):
				t.Errorf("configs do not match:\n%s", diff.ObjectReflectDiff(tc.expected, actual))
			}
		})
	}
}

const (
	kubeConfig1 = `---
apiVersion: v1
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: https://api.build01.ci.devcluster.openshift.com:6443
  name: api-build01-ci-devcluster-openshift-com:6443
contexts:
- context:
    cluster: api-build01-ci-devcluster-openshift-com:6443
    namespace: ci
    user: system:serviceaccount:ci:plank/api-build01-ci-devcluster-openshift-com:6443
  name: ci/api-build01-ci-devcluster-openshift-com:6443
current-context: ci/api-build01-ci-devcluster-openshift-com:6443
kind: Config
preferences: {}
users:
- name: system:serviceaccount:ci:plank/api-build01-ci-devcluster-openshift-com:6443
  user:
    token: foobar
`
)

func TestKubeConfigs(t *testing.T) {

	dir, err := ioutil.TempDir("", "test-")
	if err != nil {
		t.Fatal("failed to create temp dir")
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("failed to remove temp dir: '%s'", dir)
		}
	}()

	cases := []struct {
		name             string
		kubeConfigString string
		kubeConfigPath   string
		expectedConfigs  map[string]rest.Config
		expectedContext  string
		expectedErr      error
	}{
		{
			name:             "kubeconfig with 1 context",
			kubeConfigString: kubeConfig1,
			kubeConfigPath:   path.Join(dir, "config1"),
			expectedConfigs: map[string]rest.Config{
				"ci/api-build01-ci-devcluster-openshift-com:6443": {
					Host:        "https://api.build01.ci.devcluster.openshift.com:6443",
					BearerToken: "foobar",
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				},
			},
			expectedContext: "ci/api-build01-ci-devcluster-openshift-com:6443",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ioutil.WriteFile(tc.kubeConfigPath, []byte(kubeConfig1), 0644); err != nil {
				t.Fatalf("failed to write kubeconfig file: '%s'", tc.kubeConfigPath)
			}
			actual, context, err := kubeConfigs(tc.kubeConfigPath)

			if !equality.Semantic.DeepEqual(actual, tc.expectedConfigs) {
				t.Errorf("configs do not match:\n%s", diff.ObjectReflectDiff(actual, tc.expectedConfigs))
			}
			if !equality.Semantic.DeepEqual(context, tc.expectedContext) {
				t.Errorf("current contexts do not match:\n%s", diff.ObjectReflectDiff(context, tc.expectedContext))
			}
			if !equality.Semantic.DeepEqual(err, tc.expectedErr) {
				t.Errorf("errors do not match:\n%s", diff.ObjectReflectDiff(err, tc.expectedErr))
			}

		})
	}

}

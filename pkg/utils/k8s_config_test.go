/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2/textlogger"
)

const testKubeCfg1 = `
apiVersion: v1
kind: Config
clusters:
- name: cloud1
  cluster:
    server: https://10.10.10.1:8443
- name: cloud2
  cluster:
    server: https://10.10.10.2:8443
contexts:
- name: cloud1
  context:
    cluster: cloud1
    namespace: default
- name: cloud2
  context:
    cluster: cloud2
    namespace: default
current-context: cloud1
`

const testKubeCfg2 = `
apiVersion: v1
kind: Config
clusters:
- name: cloud3
  cluster:
    server: https://10.10.10.3:8443
contexts:
- name: cloud3
  context:
    cluster: cloud3
    namespace: default
current-context: cloud3
`

var testLogger = textlogger.NewLogger(textlogger.NewConfig())

func TestGetK8sConfig(t *testing.T) {
	testCases := []struct {
		name         string
		envCfg       string
		kubeCfg      string
		kubeCtx      string
		expectedErr  string
		expectedHost string
	}{
		{
			name:         "Case 1: default context",
			kubeCfg:      testKubeCfg1,
			expectedHost: "https://10.10.10.1:8443",
		},
		{
			name:         "Case 2: set context",
			kubeCfg:      testKubeCfg1,
			kubeCtx:      "cloud2",
			expectedHost: "https://10.10.10.2:8443",
		},
		{
			name:        "Case 3: wrong context",
			kubeCfg:     testKubeCfg1,
			kubeCtx:     "none",
			expectedErr: `no kubecontext "none"`,
		},
		{
			name:         "Case 4: KUBECONFIG precedence",
			kubeCfg:      testKubeCfg1,
			envCfg:       testKubeCfg2,
			expectedHost: "https://10.10.10.3:8443",
		},
		{
			name:         "Case 5: KUBECONFIG only",
			envCfg:       testKubeCfg1,
			expectedHost: "https://10.10.10.1:8443",
		},
		{
			name:         "Case 6: KUBECONFIG only with context",
			envCfg:       testKubeCfg1,
			kubeCtx:      "cloud2",
			expectedHost: "https://10.10.10.2:8443",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Unsetenv("KUBECONFIG")
			if len(tc.envCfg) != 0 {
				f, err := os.CreateTemp("", "test")
				require.NoError(t, err)
				defer func() { _ = os.Remove(f.Name()) }()

				_, err = f.Write([]byte(tc.envCfg))
				require.NoError(t, err)

				err = os.Setenv("KUBECONFIG", f.Name())
				require.NoError(t, err)
			}

			var cfgPath string
			if len(tc.kubeCfg) != 0 {
				f, err := os.CreateTemp("", "test")
				require.NoError(t, err)
				defer func() { _ = os.Remove(f.Name()) }()

				_, err = f.Write([]byte(tc.kubeCfg))
				require.NoError(t, err)

				cfgPath = f.Name()
			}
			cfg, err := GetK8sConfig(testLogger, cfgPath, tc.kubeCtx)
			if len(tc.expectedErr) != 0 {
				require.EqualError(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedHost, cfg.Host)
			}
		})
	}
}

func TestUpdateFileLocation(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		expextedPath string
	}{
		{
			name: "Case 1: empty path",
		},
		{
			name:         "Case 2: abs path",
			path:         "/my/config",
			expextedPath: "/my/config",
		},
		{
			name:         "Case 3: relative path",
			path:         "config",
			expextedPath: "/my/dir/config",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expextedPath, updateFileLocation(tc.path, "/my/dir"))
		})
	}
}

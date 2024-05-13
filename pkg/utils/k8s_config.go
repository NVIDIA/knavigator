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
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	cfg "github.com/NVIDIA/knavigator/pkg/config"
)

func GetK8sConfig(log logr.Logger, cfg *cfg.KubeConfig) (*rest.Config, error) {
	// checking in-cluster kubeconfig
	restConfig, err := rest.InClusterConfig()
	if err == nil {
		restConfig.Burst = cfg.Burst
		restConfig.QPS = cfg.QPS
		log.Info("Using in-cluster kubeconfig")
		return restConfig, err
	}

	// checking external kubeconfig
	log.Info("Using external kubeconfig")
	configAccess := clientcmd.NewDefaultPathOptions()
	if len(cfg.KubeConfigPath) != 0 {
		configAccess.GlobalFile = cfg.KubeConfigPath
	}

	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return nil, err
	}
	// update cert file location if needed
	for _, info := range config.AuthInfos {
		if len(info.LocationOfOrigin) != 0 {
			dir := filepath.Dir(info.LocationOfOrigin)
			info.ClientCertificate = updateFileLocation(info.ClientCertificate, dir)
			info.ClientKey = updateFileLocation(info.ClientKey, dir)
		}
	}

	if len(cfg.KubeCtx) != 0 {
		log.Info("Setting kubecontext", "name", cfg.KubeCtx)

		err = validateKubeContext(config, cfg.KubeCtx)
		if err != nil {
			return nil, err
		}
		config.CurrentContext = cfg.KubeCtx
	}

	return clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func validateKubeContext(config *clientcmdapi.Config, kubectx string) error {
	for name := range config.Contexts {
		if name == kubectx {
			return nil
		}
	}

	return fmt.Errorf("no kubecontext %q", kubectx)
}

func updateFileLocation(name, dir string) string {
	// if absolute name - do not change
	if len(name) == 0 || filepath.IsAbs(name) {
		return name
	}
	// append to dir
	return filepath.Join(dir, name)
}

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
	"bytes"
	"flag"
	"fmt"
	"sync/atomic"
	"text/template"

	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

func Flag2Verbosity(f *flag.Flag) int {
	if f == nil || f.Value == nil {
		return 0
	}

	obj, ok := f.Value.(*klog.Level)
	if !ok {
		return 0
	}

	return int(obj.Get().(klog.Level))
}

func ExecTemplate(tpl *template.Template, params map[string]interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := tpl.Execute(buf, params); err != nil {
		return nil, fmt.Errorf("failed to execute template: %v", err)
	}

	dat, err := yaml.YAMLToJSON(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON for template: %v", err)
	}

	return dat, nil
}

var objID int64

func SetObjectID(val int64) {
	atomic.StoreInt64(&objID, val)
}

func GenerateNames(pattern string, n int, params map[string]interface{}) ([]string, error) {
	names := make([]string, n)
	if len(pattern) != 0 {
		tpl, err := template.New("name").Parse(pattern)
		if err != nil {
			return nil, err
		}

		for i := 0; i < n; i++ {
			params["_ENUM_"] = atomic.AddInt64(&objID, 1)
			buf := new(bytes.Buffer)
			if err := tpl.Execute(buf, params); err != nil {
				return nil, fmt.Errorf("failed to execute template %s: %v", pattern, err)
			}
			names[i] = buf.String()
		}
	}
	return names, nil
}

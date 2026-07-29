/*
Copyright 2026 The Noctaya Authors.

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

package runtime

import (
	"bytes"
	"fmt"
	"text/template"
)

// TemplateData is the context available to InferenceRuntime arg/env templates.
type TemplateData struct {
	Model   ModelData
	Service ServiceData
}

type ModelData struct{ Path string }
type ServiceData struct{ Name, Namespace string }

func Render(tmpl string, data TemplateData) (string, error) {
	t, err := template.New("tmpl").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", tmpl, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template %q: %w", tmpl, err)
	}
	return buf.String(), nil
}

func RenderAll(tmpls []string, data TemplateData) ([]string, error) {
	out := make([]string, 0, len(tmpls))
	for _, tmpl := range tmpls {
		rendered, err := Render(tmpl, data)
		if err != nil {
			return nil, err
		}
		out = append(out, rendered)
	}
	return out, nil
}

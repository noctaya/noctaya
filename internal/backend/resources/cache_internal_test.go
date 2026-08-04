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

package resources

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

func TestOCIPromotionIsAtomicAndRetrySafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated OCI promotion runs in Linux model-serving containers")
	}
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	destination := filepath.Join(root, "cache", "model")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "config.json"), []byte("complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination+".partial", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination+".partial", "stale"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := backendruntime.ResolvedModel{
		Path: destination, ReadyPath: filepath.Join(destination, ".noctaya-ready"),
	}
	if output, err := exec.Command("python3", "-c", ociPromotionScriptAt(model, staging)).CombinedOutput(); err != nil {
		t.Fatalf("promotion failed: %v\n%s", err, output)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "config.json")); err != nil || string(content) != "complete" {
		t.Fatalf("promoted content=%q err=%v", content, err)
	}
	if _, err := os.Stat(model.ReadyPath); err != nil {
		t.Fatalf("readiness marker missing: %v", err)
	}
	if _, err := os.Stat(destination + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial directory survived successful promotion: %v", err)
	}
}

func TestOCIPromotionRejectsSymbolicLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated OCI promotion runs in Linux model-serving containers")
	}
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	destination := filepath.Join(root, "cache", "model")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(staging, "weights")); err != nil {
		t.Fatal(err)
	}
	model := backendruntime.ResolvedModel{
		Path: destination, ReadyPath: filepath.Join(destination, ".noctaya-ready"),
	}
	if output, err := exec.Command("python3", "-c", ociPromotionScriptAt(model, staging)).CombinedOutput(); err == nil {
		t.Fatalf("promotion accepted a symbolic link:\n%s", output)
	}
	if _, err := os.Stat(model.ReadyPath); !os.IsNotExist(err) {
		t.Fatalf("failed promotion wrote readiness marker: %v", err)
	}
}

func ociPromotionScriptAt(model backendruntime.ResolvedModel, staging string) string {
	script := ociPromotionScript(model)
	return strings.Replace(script, "src = \"/staging/model\"", "src = "+strconv.Quote(staging), 1)
}

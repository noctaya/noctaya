//go:build e2e

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

// Package scaletozero drives Noctaya's complete no-accelerator lifecycle against
// a disposable Kind cluster, real manager manifests, KEDA, and a CPU vLLM stub.
package scaletozero

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	namespace         = "noctaya-e2e"
	managerNamespace  = "noctaya-system"
	managerDeployment = "noctaya-controller-manager"
	fakeResource      = "noctaya.io/fake-gpu"
)

var (
	repoRoot        string
	scalerMode      string
	kustomize       string
	expectedKind    string
	managerImage    string
	gatewayImage    string
	stubImage       string
	httpClient      = &http.Client{Transport: &http.Transport{Proxy: nil}}
	diagnosticLimit = "200"
)

func TestScaleToZero(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "scale-to-zero e2e suite")
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func kubectl(args ...string) (string, error) {
	return run("kubectl", args...)
}

func mustKubectl(args ...string) string {
	output, err := kubectl(args...)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl "+strings.Join(args, " ")+":\n"+output)
	return output
}

func manifestPath(name string) string {
	return filepath.Join(repoRoot, "test", "scale-to-zero", "data", name)
}

func applyManifest(name string) {
	objects, err := os.ReadFile(manifestPath(name))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	objects = bytes.ReplaceAll(objects, []byte("noctaya.io/vllm-stub:e2e"), []byte(stubImage))
	applyYAML(objects)
}

func applyYAML(objects []byte) {
	apply := exec.Command("kubectl", "apply", "-f", "-")
	apply.Dir = repoRoot
	apply.Stdin = bytes.NewReader(objects)
	output, err := apply.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), string(output))
}

func applyManager() {
	overlay := filepath.Join(repoRoot, "test", "scale-to-zero", "kustomize", scalerMode)
	render := exec.Command(kustomize, "build", overlay)
	render.Dir = repoRoot
	objects, err := render.Output()
	Expect(err).NotTo(HaveOccurred())
	objects = bytes.ReplaceAll(objects, []byte("noctaya.io/noctaya:e2e"), []byte(managerImage))
	objects = bytes.ReplaceAll(objects, []byte("noctaya.io/noctaya-gateway:e2e"), []byte(gatewayImage))
	applyYAML(objects)
}

func backendReplicas(name string) (int, error) {
	output, err := kubectl("get", "deployment", name, "-n", namespace, "-o", "jsonpath={.spec.replicas}")
	if err != nil {
		return 0, fmt.Errorf("get backend replicas: %w: %s", err, output)
	}
	replicas, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("parse backend replicas %q: %w", output, err)
	}
	return replicas, nil
}

func backendPods(name string) (int, error) {
	output, err := kubectl("get", "pods", "-n", namespace,
		"-l", "serving.noctaya.io/llmservice="+name, "-o", "name")
	if err != nil {
		return 0, fmt.Errorf("get backend pods: %w: %s", err, output)
	}
	return len(strings.Fields(output)), nil
}

var _ = BeforeSuite(func() {
	_, file, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	var err error
	repoRoot, err = filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	Expect(err).NotTo(HaveOccurred())

	scalerMode = os.Getenv("NOCTAYA_E2E_SCALER_MODE")
	Expect([]string{"metrics-api", "external-push"}).To(ContainElement(scalerMode))

	expectedKind = os.Getenv("NOCTAYA_E2E_KIND_CLUSTER")
	Expect(expectedKind).NotTo(BeEmpty(), "run this suite through make test-e2e")
	currentContext := strings.TrimSpace(mustKubectl("config", "current-context"))
	Expect(currentContext).To(Equal("kind-"+expectedKind),
		"refusing to mutate a cluster other than the disposable E2E Kind cluster")

	kustomize = os.Getenv("NOCTAYA_E2E_KUSTOMIZE")
	Expect(kustomize).NotTo(BeEmpty(), "run this suite through make test-e2e")
	if !filepath.IsAbs(kustomize) {
		kustomize = filepath.Join(repoRoot, kustomize)
	}
	managerImage = os.Getenv("NOCTAYA_E2E_MANAGER_IMAGE")
	gatewayImage = os.Getenv("NOCTAYA_E2E_GATEWAY_IMAGE")
	stubImage = os.Getenv("NOCTAYA_E2E_STUB_IMAGE")
	Expect(managerImage).NotTo(BeEmpty())
	Expect(gatewayImage).NotTo(BeEmpty())
	Expect(stubImage).NotTo(BeEmpty())

	By("checking the external KEDA dependency")
	_, err = kubectl("get", "customresourcedefinition", "scaledobjects.keda.sh")
	Expect(err).NotTo(HaveOccurred(), "KEDA must be installed by the E2E runner")

	By("deploying the manager through the repository Kustomize configuration")
	applyManager()
	output, err := kubectl("rollout", "status", "deployment/"+managerDeployment,
		"-n", managerNamespace, "--timeout=180s")
	Expect(err).NotTo(HaveOccurred(), output)

	By("creating an empty test namespace")
	output, err = kubectl("create", "namespace", namespace)
	Expect(err).NotTo(HaveOccurred(), output)

	By("advertising a fake accelerator resource on the disposable Kind nodes")
	nodes := strings.Fields(mustKubectl("get", "nodes", "-o", "jsonpath={.items[*].metadata.name}"))
	Expect(nodes).NotTo(BeEmpty())
	patch := fmt.Sprintf(`{"status":{"capacity":{"%s":"8"}}}`, fakeResource)
	for _, node := range nodes {
		mustKubectl("patch", "node", node, "--subresource=status", "--type=merge", "-p", patch)
	}
})

var _ = ReportAfterEach(func(report SpecReport) {
	if !report.Failed() {
		return
	}
	dumpDiagnostics()
})

func dumpDiagnostics() {
	commands := []struct {
		title string
		args  []string
	}{
		{"Manager logs", []string{"logs", "deployment/" + managerDeployment, "-n", managerNamespace, "--tail=" + diagnosticLimit}},
		{"E2E objects", []string{"get", "all,llmservices,scaledobjects", "-n", namespace, "-o", "wide"}},
		{"E2E pod descriptions", []string{"describe", "pods", "-n", namespace}},
		{"Gateway logs", []string{"logs", "-n", namespace, "-l", "serving.noctaya.io/gateway",
			"--all-containers=true", "--prefix=true", "--tail=" + diagnosticLimit, "--max-log-requests=20"}},
		{"Backend logs", []string{"logs", "-n", namespace, "-l", "serving.noctaya.io/llmservice",
			"--all-containers=true", "--prefix=true", "--tail=" + diagnosticLimit, "--max-log-requests=20"}},
		{"KEDA operator logs", []string{"logs", "deployment/keda-operator", "-n", "keda", "--tail=" + diagnosticLimit}},
		{"E2E events", []string{"get", "events", "-n", namespace, "--sort-by=.lastTimestamp"}},
	}
	for _, command := range commands {
		output, err := kubectl(command.args...)
		_, _ = fmt.Fprintf(GinkgoWriter, "\n--- %s ---\n%s", command.title, output)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "\nDiagnostic command failed: %v\n", err)
		}
	}
}

type portForward struct {
	port   int
	cancel context.CancelFunc
	cmd    *exec.Cmd
	done   chan struct{}
	mu     sync.Mutex
	err    error
	once   sync.Once
}

func startPortForward(service string) *portForward {
	output, err := kubectl("wait", "--for=condition=Available", "--timeout=120s",
		"deployment/"+service+"-gateway", "-n", namespace)
	Expect(err).NotTo(HaveOccurred(), output)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	Expect(listener.Close()).To(Succeed())

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "port-forward", "--address=127.0.0.1",
		"-n", namespace, "service/"+service, fmt.Sprintf("%d:80", port))
	cmd.Dir = repoRoot
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	forward := &portForward{port: port, cancel: cancel, cmd: cmd, done: make(chan struct{})}
	err = cmd.Start()
	if err != nil {
		cancel()
	}
	Expect(err).NotTo(HaveOccurred())
	go func() {
		err := cmd.Wait()
		forward.mu.Lock()
		forward.err = err
		forward.mu.Unlock()
		close(forward.done)
	}()
	DeferCleanup(forward.stop)

	Eventually(func() error {
		select {
		case <-forward.done:
			forward.mu.Lock()
			defer forward.mu.Unlock()
			return fmt.Errorf("port-forward exited before becoming ready: %v", forward.err)
		default:
		}
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			_ = connection.Close()
		}
		return err
	}, 30*time.Second, time.Second).Should(Succeed())
	return forward
}

func (p *portForward) stop() {
	p.once.Do(func() {
		p.cancel()
		select {
		case <-p.done:
			return
		case <-time.After(5 * time.Second):
		}
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-p.done
	})
}

//go:build release

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

// Package release validates a candidate chart against the last compatible release
// on an isolated Kind cluster.
package release

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

const (
	namespace         = "noctaya-release"
	managerNamespace  = "noctaya-system"
	managerDeployment = "noctaya-controller-manager"
	releaseName       = "noctaya"
	serviceName       = "release-svc"
	fakeResource      = "noctaya.io/fake-gpu"
)

var (
	repoRoot        string
	kubectlBin      string
	helmBin         string
	kindBin         string
	containerTool   string
	expectedKind    string
	candidateChart  string
	managerImage    string
	gatewayImage    string
	stubImage       string
	previousVersion string
	httpClient      = &http.Client{Transport: &http.Transport{Proxy: nil}}
)

func TestReleaseValidation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "release validation suite")
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func kubectl(args ...string) (string, error) {
	return run(kubectlBin, args...)
}

func mustKubectl(args ...string) string {
	output, err := kubectl(args...)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl "+strings.Join(args, " ")+":\n"+output)
	return strings.TrimSpace(output)
}

func helm(args ...string) (string, error) {
	return run(helmBin, args...)
}

func mustHelm(args ...string) string {
	output, err := helm(args...)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "helm "+strings.Join(args, " ")+":\n"+output)
	return strings.TrimSpace(output)
}

func manifestPath(name string) string {
	return filepath.Join(repoRoot, "test", "release", "testdata", name)
}

func applyManifest(name string) {
	objects, err := os.ReadFile(manifestPath(name))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	objects = bytes.ReplaceAll(objects, []byte("noctaya.io/vllm-stub:e2e"), []byte(stubImage))
	apply := exec.Command(kubectlBin, "apply", "-f", "-")
	apply.Dir = repoRoot
	apply.Stdin = bytes.NewReader(objects)
	output, err := apply.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), string(output))
}

func resourceUID(resource string) string {
	return mustKubectl("get", resource, "-n", namespace, "-o", "jsonpath={.metadata.uid}")
}

func controllerUID(resource string) string {
	return mustKubectl(
		"get", resource, "-n", namespace,
		"-o", `jsonpath={.metadata.ownerReferences[?(@.controller==true)].uid}`,
	)
}

func backendReplicas() string {
	output, _ := kubectl(
		"get", "deployment", serviceName, "-n", namespace,
		"-o", "jsonpath={.spec.replicas}",
	)
	return strings.TrimSpace(output)
}

func readyPodNames(selector string) ([]string, error) {
	output, err := kubectl("get", "pods", "-n", namespace, "-l", selector, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("get pods for %q: %w: %s", selector, err, output)
	}
	var pods corev1.PodList
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		return nil, fmt.Errorf("decode pods for %q: %w", selector, err)
	}
	names := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp != nil {
			continue
		}
		for _, condition := range pods.Items[i].Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				names = append(names, pods.Items[i].Name)
				break
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

func managerPods() ([]corev1.Pod, error) {
	output, err := kubectl(
		"get", "pods", "-n", managerNamespace,
		"-l", "app.kubernetes.io/component=controller-manager", "-o", "json",
	)
	if err != nil {
		return nil, fmt.Errorf("get manager pods: %w: %s", err, output)
	}
	var pods corev1.PodList
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		return nil, fmt.Errorf("decode manager pods: %w", err)
	}
	current := make([]corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil {
			current = append(current, pods.Items[i])
		}
	}
	return current, nil
}

func leaderHolder() (string, error) {
	output, err := kubectl(
		"get", "lease", "6d7012cb.noctaya.io", "-n", managerNamespace,
		"-o", "jsonpath={.spec.holderIdentity}",
	)
	if err != nil {
		return "", fmt.Errorf("get leader lease: %w: %s", err, output)
	}
	return strings.TrimSpace(output), nil
}

func holderPod(holder string, pods []corev1.Pod) string {
	for i := range pods {
		if holder == pods[i].Name || strings.HasPrefix(holder, pods[i].Name+"_") {
			return pods[i].Name
		}
	}
	return ""
}

func parseImage(image string) (registry, repository, tag string, err error) {
	slash := strings.LastIndexByte(image, '/')
	colon := strings.LastIndexByte(image, ':')
	if slash <= 0 || colon <= slash+1 || colon == len(image)-1 {
		return "", "", "", fmt.Errorf("image %q must use registry/repository:tag", image)
	}
	return image[:slash], image[slash+1 : colon], image[colon+1:], nil
}

func upgradeCandidate() {
	managerRegistry, managerRepository, managerTag, err := parseImage(managerImage)
	Expect(err).NotTo(HaveOccurred())
	gatewayRegistry, gatewayRepository, gatewayTag, err := parseImage(gatewayImage)
	Expect(err).NotTo(HaveOccurred())
	Expect(gatewayRegistry).To(Equal(managerRegistry))
	Expect(gatewayTag).To(Equal(managerTag))

	mustHelm(
		"upgrade", releaseName, candidateChart,
		"--namespace", managerNamespace,
		"--reset-values",
		"--set", "image.registry="+managerRegistry,
		"--set", "image.operator="+managerRepository,
		"--set", "image.gateway="+gatewayRepository,
		"--set", "image.tag="+managerTag,
		"--set", "image.pullPolicy=Never",
		"--set", "operator.replicas=2",
		"--set", "operator.leaderElect=true",
		"--set", "gateway.replicas=1",
		"--wait",
		"--timeout", "5m",
	)
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

func startPortForward() *portForward {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	Expect(listener.Close()).To(Succeed())

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(
		ctx, kubectlBin, "port-forward", "--address=127.0.0.1",
		"-n", namespace, "service/"+serviceName, fmt.Sprintf("%d:80", port),
	)
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
		connection, dialErr := net.DialTimeout(
			"tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second,
		)
		if dialErr == nil {
			_ = connection.Close()
		}
		return dialErr
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

func requestChat() (int, string, error) {
	forward := startPortForward()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", forward.port),
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"release validation"}]}`),
	)
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = response.Body.Close() }()
	var body strings.Builder
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return response.StatusCode, body.String(), err
	}
	return response.StatusCode, body.String(), nil
}

var _ = BeforeSuite(func() {
	_, file, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	var err error
	repoRoot, err = filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	Expect(err).NotTo(HaveOccurred())

	kubectlBin = os.Getenv("NOCTAYA_RELEASE_KUBECTL")
	helmBin = os.Getenv("NOCTAYA_RELEASE_HELM")
	kindBin = os.Getenv("NOCTAYA_RELEASE_KIND")
	containerTool = os.Getenv("NOCTAYA_RELEASE_CONTAINER_TOOL")
	expectedKind = os.Getenv("NOCTAYA_RELEASE_KIND_CLUSTER")
	candidateChart = os.Getenv("NOCTAYA_RELEASE_CANDIDATE_CHART")
	managerImage = os.Getenv("NOCTAYA_RELEASE_MANAGER_IMAGE")
	gatewayImage = os.Getenv("NOCTAYA_RELEASE_GATEWAY_IMAGE")
	stubImage = os.Getenv("NOCTAYA_RELEASE_STUB_IMAGE")
	previousVersion = os.Getenv("NOCTAYA_RELEASE_PREVIOUS_VERSION")
	for name, value := range map[string]string{
		"NOCTAYA_RELEASE_KUBECTL":          kubectlBin,
		"NOCTAYA_RELEASE_HELM":             helmBin,
		"NOCTAYA_RELEASE_KIND":             kindBin,
		"NOCTAYA_RELEASE_CONTAINER_TOOL":   containerTool,
		"NOCTAYA_RELEASE_KIND_CLUSTER":     expectedKind,
		"NOCTAYA_RELEASE_CANDIDATE_CHART":  candidateChart,
		"NOCTAYA_RELEASE_MANAGER_IMAGE":    managerImage,
		"NOCTAYA_RELEASE_GATEWAY_IMAGE":    gatewayImage,
		"NOCTAYA_RELEASE_STUB_IMAGE":       stubImage,
		"NOCTAYA_RELEASE_PREVIOUS_VERSION": previousVersion,
	} {
		Expect(value).NotTo(BeEmpty(), name+" must be provided by make test-release")
	}

	currentContext := mustKubectl("config", "current-context")
	Expect(currentContext).To(Equal("kind-"+expectedKind),
		"refusing to mutate a cluster other than the disposable release Kind cluster")
	Expect(filepath.Clean(candidateChart)).To(Equal(filepath.Join(repoRoot, "charts", "noctaya")))

	By("checking the baseline release and external KEDA dependency")
	Expect(mustHelm(
		"get", "metadata", releaseName, "-n", managerNamespace, "-o", "json",
	)).To(ContainSubstring(previousVersion))
	_, err = kubectl("get", "customresourcedefinition", "scaledobjects.keda.sh")
	Expect(err).NotTo(HaveOccurred())

	By("creating the release-validation workload")
	mustKubectl("create", "namespace", namespace)
	nodes := strings.Fields(mustKubectl(
		"get", "nodes", "-o", "jsonpath={.items[*].metadata.name}",
	))
	Expect(nodes).To(HaveLen(3))
	patch := fmt.Sprintf(`{"status":{"capacity":{"%s":"8"}}}`, fakeResource)
	for _, node := range nodes {
		mustKubectl("patch", "node", node, "--subresource=status", "--type=merge", "-p", patch)
	}
	applyManifest("runtime.yaml")
	applyManifest("llmservice.yaml")
})

var _ = ReportAfterEach(func(report SpecReport) {
	if !report.Failed() {
		return
	}
	for _, command := range [][]string{
		{"get", "pods,poddisruptionbudgets", "--all-namespaces", "-o", "wide"},
		{"get", "llmservices,scaledobjects", "--all-namespaces", "-o", "yaml"},
		{"get", "events", "-n", namespace, "--sort-by=.lastTimestamp"},
		{"logs", "deployment/" + managerDeployment, "-n", managerNamespace, "--all-pods=true", "--prefix=true", "--tail=300"},
	} {
		output, err := kubectl(command...)
		_, _ = fmt.Fprintf(GinkgoWriter, "\n--- kubectl %s ---\n%s", strings.Join(command, " "), output)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "\nDiagnostic command failed: %v\n", err)
		}
	}
})

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

package release

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Release candidate", Ordered, func() {
	var resourceUIDs map[string]string

	controlledResources := []string{
		"deployment/" + serviceName,
		"deployment/" + serviceName + "-gateway",
		"service/" + serviceName,
		"service/" + serviceName + "-backend",
		"service/" + serviceName + "-scaler",
		"scaledobject/" + serviceName,
	}

	assertContinuity := func() {
		llmUID := resourceUIDs["llmservice/"+serviceName]
		Expect(resourceUID("llmservice/" + serviceName)).To(Equal(llmUID))
		for _, resource := range controlledResources {
			Expect(resourceUID(resource)).To(Equal(resourceUIDs[resource]), resource+" UID changed")
			Expect(controllerUID(resource)).To(Equal(llmUID), resource+" lost its controller owner")
		}
		Expect(mustKubectl(
			"get", "llmservice", serviceName, "-n", namespace,
			"-o", "jsonpath={.status.resolvedRuntime}",
		)).To(Equal("release-vllm-stub"))
	}

	assertInference := func() {
		code, body, err := requestChat()
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Equal(http.StatusOK), body)
		Expect(body).To(ContainSubstring("[DONE]"))
	}

	It("serves a workload on the last compatible release", func() {
		Eventually(backendReplicas, 2*time.Minute, 2*time.Second).Should(Equal("0"))
		Eventually(func() string {
			output, _ := kubectl(
				"get", "deployment", serviceName+"-gateway", "-n", namespace,
				"-o", "jsonpath={.status.readyReplicas}",
			)
			return strings.TrimSpace(output)
		}, 2*time.Minute, 2*time.Second).Should(Equal("1"))

		resourceUIDs = map[string]string{
			"llmservice/" + serviceName: resourceUID("llmservice/" + serviceName),
		}
		for _, resource := range controlledResources {
			resourceUIDs[resource] = resourceUID(resource)
		}
		assertContinuity()
		assertInference()
		Eventually(backendReplicas, 2*time.Minute, 2*time.Second).Should(Equal("0"))
	})

	It("upgrades CRDs and the Helm release without replacing owned resources", func() {
		crdDir := candidateChart + "/crds"
		output, err := kubectl(
			"apply", "--server-side", "--force-conflicts", "--dry-run=server",
			"--field-manager=noctaya-release-validator", "-f", crdDir,
		)
		Expect(err).NotTo(HaveOccurred(), output)
		mustKubectl(
			"apply", "--server-side", "--force-conflicts",
			"--field-manager=noctaya-release-validator", "-f", crdDir,
		)

		upgradeCandidate()
		Expect(mustHelm(
			"history", releaseName, "-n", managerNamespace,
			"-o", "json",
		)).To(ContainSubstring(`"revision":2`))
		Eventually(func() string {
			output, _ := kubectl(
				"get", "deployment", managerDeployment, "-n", managerNamespace,
				"-o", "jsonpath={.status.readyReplicas}",
			)
			return strings.TrimSpace(output)
		}, 3*time.Minute, time.Second).Should(Equal("2"))

		pods, err := managerPods()
		Expect(err).NotTo(HaveOccurred())
		Expect(pods).To(HaveLen(2))
		Expect(pods[0].Spec.NodeName).NotTo(Equal(pods[1].Spec.NodeName))
		Expect(mustKubectl(
			"get", "poddisruptionbudget", managerDeployment, "-n", managerNamespace,
			"-o", "jsonpath={.spec.minAvailable}",
		)).To(Equal("1"))
		assertContinuity()
		assertInference()
	})

	It("rolls back the controller while preserving the upgraded API and workload", func() {
		mustHelm(
			"rollback", releaseName, "1", "-n", managerNamespace,
			"--wait", "--timeout", "5m",
		)
		Eventually(func() string {
			output, _ := kubectl(
				"get", "deployment", managerDeployment, "-n", managerNamespace,
				"-o", "jsonpath={.status.readyReplicas}",
			)
			return strings.TrimSpace(output)
		}, 3*time.Minute, time.Second).Should(Equal("1"))
		Expect(mustKubectl(
			"get", "customresourcedefinition", "llmservices.serving.noctaya.io",
			"-o", `jsonpath={.spec.versions[?(@.name=="v1alpha1")].schema.openAPIV3Schema.properties.spec.properties.endpoint.properties.maxQueue.type}`,
		)).To(Equal("integer"), "Helm rollback must not pretend that CRD changes were reverted")

		mustKubectl(
			"patch", "llmservice", serviceName, "-n", namespace,
			"--type=merge", "-p", `{"spec":{"scaling":{"target":2}}}`,
		)
		Eventually(func() string {
			output, _ := kubectl(
				"get", "scaledobject", serviceName, "-n", namespace,
				"-o", "jsonpath={.spec.triggers[0].metadata.targetValue}",
			)
			return strings.TrimSpace(output)
		}, time.Minute, time.Second).Should(Equal("2"),
			"the previous controller must reconcile against the upgraded compatible CRD")
		assertContinuity()
		assertInference()

		upgradeCandidate()
		mustKubectl(
			"patch", "llmservice", serviceName, "-n", namespace,
			"--type=merge", "-p", `{"spec":{"scaling":{"target":1}}}`,
		)
		Eventually(func() string {
			output, _ := kubectl(
				"get", "scaledobject", serviceName, "-n", namespace,
				"-o", "jsonpath={.spec.triggers[0].metadata.targetValue}",
			)
			return strings.TrimSpace(output)
		}, time.Minute, time.Second).Should(Equal("1"))
		assertContinuity()
	})

	It("recovers reconciliation after replacing the active operator", func() {
		Eventually(func() int {
			pods, _ := managerPods()
			return len(pods)
		}, 2*time.Minute, time.Second).Should(Equal(2))
		pods, err := managerPods()
		Expect(err).NotTo(HaveOccurred())
		holder, err := leaderHolder()
		Expect(err).NotTo(HaveOccurred())
		active := holderPod(holder, pods)
		Expect(active).NotTo(BeEmpty())

		mustKubectl(
			"patch", "llmservice", serviceName, "-n", namespace,
			"--type=merge", "-p", `{"spec":{"scaling":{"target":2}}}`,
		)
		started := time.Now()
		mustKubectl("delete", "pod", active, "-n", managerNamespace, "--wait=false")
		Eventually(func() string {
			current, getErr := leaderHolder()
			if getErr != nil || current == holder {
				return ""
			}
			return current
		}, 30*time.Second, 250*time.Millisecond).ShouldNot(BeEmpty())
		Eventually(func() string {
			output, _ := kubectl(
				"get", "scaledobject", serviceName, "-n", namespace,
				"-o", "jsonpath={.spec.triggers[0].metadata.targetValue}",
			)
			return strings.TrimSpace(output)
		}, time.Minute, time.Second).Should(Equal("2"))
		handoff := time.Since(started)
		Expect(handoff).To(BeNumerically("<=", 30*time.Second))
		_, _ = fmt.Fprintf(
			GinkgoWriter, "Candidate operator recovered leadership and reconciliation in %s\n",
			handoff.Round(time.Millisecond),
		)
		assertContinuity()
	})

	It("recovers serving after gateway and backend replacement", func() {
		Eventually(readyPodNames, 2*time.Minute, time.Second).
			WithArguments("serving.noctaya.io/gateway=" + serviceName).
			Should(HaveLen(1))
		gateways, err := readyPodNames("serving.noctaya.io/gateway=" + serviceName)
		Expect(err).NotTo(HaveOccurred())
		oldGateway := gateways[0]
		mustKubectl("delete", "pod", oldGateway, "-n", namespace, "--wait=false")
		Eventually(readyPodNames, 2*time.Minute, time.Second).
			WithArguments("serving.noctaya.io/gateway=" + serviceName).
			Should(And(HaveLen(1), Not(ContainElement(oldGateway))))
		assertInference()

		Eventually(readyPodNames, 2*time.Minute, time.Second).
			WithArguments("app.kubernetes.io/name=" + serviceName).
			Should(HaveLen(1))
		backends, err := readyPodNames("app.kubernetes.io/name=" + serviceName)
		Expect(err).NotTo(HaveOccurred())
		oldBackend := backends[0]
		mustKubectl("delete", "pod", oldBackend, "-n", namespace, "--wait=false")
		Eventually(readyPodNames, 2*time.Minute, time.Second).
			WithArguments("app.kubernetes.io/name=" + serviceName).
			Should(And(HaveLen(1), Not(ContainElement(oldBackend))))
		assertInference()
		assertContinuity()
	})

	It("reconciles and serves after a Kind worker-node restart", func() {
		output, err := run(kindBin, "get", "nodes", "--name", expectedKind)
		Expect(err).NotTo(HaveOccurred(), output)
		var worker string
		for _, node := range strings.Fields(output) {
			if strings.HasSuffix(node, "-worker") {
				worker = node
				break
			}
		}
		Expect(worker).NotTo(BeEmpty())

		output, err = run(containerTool, "stop", worker)
		Expect(err).NotTo(HaveOccurred(), output)
		workerStopped := true
		DeferCleanup(func() {
			if workerStopped {
				_, _ = run(containerTool, "start", worker)
			}
		})
		Eventually(func() string {
			status, _ := kubectl(
				"get", "node", worker,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			return strings.TrimSpace(status)
		}, 90*time.Second, 2*time.Second).ShouldNot(Equal("True"))

		output, err = run(containerTool, "start", worker)
		Expect(err).NotTo(HaveOccurred(), output)
		workerStopped = false
		Eventually(func() string {
			status, _ := kubectl(
				"get", "node", worker,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			return strings.TrimSpace(status)
		}, 3*time.Minute, 2*time.Second).Should(Equal("True"))

		mustKubectl(
			"patch", "llmservice", serviceName, "-n", namespace,
			"--type=merge", "-p", `{"spec":{"scaling":{"target":3}}}`,
		)
		Eventually(func() string {
			target, _ := kubectl(
				"get", "scaledobject", serviceName, "-n", namespace,
				"-o", "jsonpath={.spec.triggers[0].metadata.targetValue}",
			)
			return strings.TrimSpace(target)
		}, 2*time.Minute, time.Second).Should(Equal("3"))
		assertInference()
		assertContinuity()
	})
})

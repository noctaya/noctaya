//go:build e2e

/*
Copyright 2026 The Hearth Authors.

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

package scaletozero

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type chatResult struct {
	code int
	body string
	err  error
}

func chat(port, tokens int, stream bool, timeout time.Duration, firstToken chan<- struct{}) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)
	if tokens > 0 {
		url += fmt.Sprintf("?tokens=%d", tokens)
	}
	body := fmt.Sprintf(`{"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = response.Body.Close() }()

	if !stream {
		content, err := io.ReadAll(response.Body)
		return response.StatusCode, string(content), err
	}

	var content strings.Builder
	var signaled sync.Once
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		content.WriteString(line)
		content.WriteByte('\n')
		if firstToken != nil && strings.HasPrefix(line, "data: {") {
			signaled.Do(func() { close(firstToken) })
		}
	}
	if err := scanner.Err(); err != nil {
		return response.StatusCode, content.String(), err
	}
	return response.StatusCode, content.String(), nil
}

func requestChat(port, tokens int, stream bool, timeout time.Duration) (int, string, error) {
	return chat(port, tokens, stream, timeout, nil)
}

var _ = Describe("scale-to-zero loop", Ordered, func() {
	BeforeAll(func() {
		applyManifest("runtime.yaml")
		applyManifest("llmservice.yaml")
	})

	if os.Getenv("HEARTH_E2E_SCALER_MODE") == "metrics-api" {
		It("serves authenticated manager metrics", func() {
			applyManifest("metrics.yaml")
			Eventually(func() string {
				output, _ := kubectl("get", "pod", "metrics-reader", "-n", namespace,
					"-o", "jsonpath={.status.phase}")
				return output
			}, 2*time.Minute, 2*time.Second).Should(Equal("Succeeded"))
			logs := mustKubectl("logs", "metrics-reader", "-n", namespace)
			Expect(logs).To(ContainSubstring("controller_runtime_"))
		})
	}

	It("holds the idle backend at zero", func() {
		Eventually(backendReplicas, 2*time.Minute, 3*time.Second).
			WithArguments("stub-svc").
			Should(Equal(0), "KEDA should hold the backend at zero while idle")
		Eventually(backendPods, 2*time.Minute, 2*time.Second).
			WithArguments("stub-svc").
			Should(Equal(0), "the initial backend pod must finish terminating before activation")
	})

	It("renders the selected KEDA scaler transport", func() {
		Eventually(func() string {
			output, _ := kubectl("get", "scaledobject", "stub-svc", "-n", namespace,
				"-o", "jsonpath={.spec.triggers[0].type}")
			return output
		}, time.Minute, 2*time.Second).Should(Equal(scalerMode))

		if scalerMode == "metrics-api" {
			output, err := kubectl("get", "service", "stub-svc-scaler", "-n", namespace,
				"--ignore-not-found", "-o", "name")
			Expect(err).NotTo(HaveOccurred(), output)
			Expect(strings.TrimSpace(output)).To(BeEmpty(),
				"metrics-api mode must not create an internal scaler Service")
			return
		}

		address := mustKubectl("get", "scaledobject", "stub-svc", "-n", namespace,
			"-o", "jsonpath={.spec.triggers[0].metadata.scalerAddress}")
		Expect(address).To(Equal("stub-svc-scaler.hearth-e2e.svc:9090"))
		mustKubectl("get", "service", "stub-svc-scaler", "-n", namespace)

		forward := startPortForward("stub-svc")
		Eventually(func() string {
			response, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", forward.port))
			if err != nil {
				return ""
			}
			defer func() { _ = response.Body.Close() }()
			content, err := io.ReadAll(response.Body)
			if err != nil {
				return ""
			}
			return string(content)
		}, time.Minute, 2*time.Second).Should(ContainSubstring("hearth_gateway_scaler_streams 1"))
	})

	It("wakes on a cold streaming request (0→1)", func() {
		forward := startPortForward("stub-svc")

		code, body, err := requestChat(forward.port, 0, true, 90*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Equal(http.StatusOK))
		Expect(body).To(ContainSubstring("[DONE]"), "the completion stream must not be truncated")
		Eventually(backendReplicas, 30*time.Second, time.Second).
			WithArguments("stub-svc").
			Should(BeNumerically(">=", 1), "the request should wake the backend")
	})

	It("scales out under concurrent load (1→2)", func() {
		forward := startPortForward("stub-svc")

		const requests = 4
		results := make(chan chatResult, requests)
		for range requests {
			go func() {
				code, body, err := requestChat(forward.port, 120, true, 2*time.Minute)
				results <- chatResult{code: code, body: body, err: err}
			}()
		}

		Eventually(backendReplicas, 90*time.Second, 3*time.Second).
			WithArguments("stub-svc").
			Should(Equal(2), "concurrent load should scale the backend to its maximum")
		for range requests {
			var result chatResult
			Eventually(results, 2*time.Minute).Should(Receive(&result))
			Expect(result.err).NotTo(HaveOccurred())
			Expect(result.code).To(Equal(http.StatusOK))
			Expect(result.body).To(ContainSubstring("[DONE]"), "a concurrent stream was truncated")
		}
	})

	It("returns to zero after load drains (N→0)", func() {
		Eventually(backendReplicas, 2*time.Minute, 3*time.Second).
			WithArguments("stub-svc").
			Should(Equal(0), "the backend should return to zero once idle")
	})

	It("drains an active stream during backend pod termination", func() {
		applyManifest("drain.yaml")
		Eventually(backendReplicas, 2*time.Minute, 3*time.Second).
			WithArguments("stub-drain").
			Should(Equal(0), "the drain service should start at zero")

		forward := startPortForward("stub-drain")

		firstToken := make(chan struct{})
		result := make(chan chatResult, 1)
		go func() {
			code, body, err := chat(forward.port, 80, true, 2*time.Minute, firstToken)
			result <- chatResult{code: code, body: body, err: err}
		}()

		By("waiting until the gateway is actively proxying backend tokens")
		Eventually(firstToken, 2*time.Minute).Should(BeClosed())

		By("terminating the backend pod while the stream is active")
		output, err := kubectl("delete", "pod", "-n", namespace,
			"-l", "serving.hearth.dev/llmservice=stub-drain", "--wait=false")
		Expect(err).NotTo(HaveOccurred(), output)

		var completed chatResult
		Eventually(result, 90*time.Second).Should(Receive(&completed))
		Expect(completed.err).NotTo(HaveOccurred())
		Expect(completed.code).To(Equal(http.StatusOK))
		Expect(completed.body).To(ContainSubstring("[DONE]"), "the active stream must drain to completion")
	})

	It("returns 503 promptly when a cold backend cannot schedule", func() {
		applyManifest("reject.yaml")
		forward := startPortForward("stub-503")

		code, _, err := requestChat(forward.port, 0, false, 30*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(Equal(http.StatusServiceUnavailable))
	})
})

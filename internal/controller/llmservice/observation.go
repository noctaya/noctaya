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

package llmservice

import (
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

const (
	repeatedRestartThreshold int32 = 2

	reasonCrashLoopBackOff         = "CrashLoopBackOff"
	reasonContainerRestarting      = "ContainerRestarting"
	reasonImagePullBackOff         = "ImagePullBackOff"
	reasonImagePullFailed          = "ImagePullFailed"
	reasonModelLoading             = "ModelLoading"
	reasonOOMKilled                = "OOMKilled"
	reasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	reasonSchedulingDelayed        = "SchedulingDelayed"
)

type backendObservation struct {
	reason   string
	message  string
	degraded bool
}

func observeBackend(dep *appsv1.Deployment, pods []corev1.Pod) backendObservation {
	observations := make([]backendObservation, 0, len(pods))
	for i := range pods {
		if !activeBackendPod(&pods[i]) {
			continue
		}
		observations = append(observations, observeBackendPod(&pods[i]))
	}
	if (dep.Status.Replicas > 0 || dep.Status.ReadyReplicas > 0 || len(observations) > 0) &&
		deploymentProgressDeadlineExceeded(dep) {
		return backendObservation{
			reason:   reasonProgressDeadlineExceeded,
			message:  "Backend Deployment exceeded its progress deadline; inspect its Pods and events",
			degraded: true,
		}
	}
	slices.SortFunc(observations, func(a, b backendObservation) int {
		if d := observationPriority(a.reason) - observationPriority(b.reason); d != 0 {
			return d
		}
		if a.message < b.message {
			return -1
		}
		if a.message > b.message {
			return 1
		}
		return 0
	})
	if len(observations) > 0 {
		return observations[0]
	}

	return backendObservation{
		reason:  "Activating",
		message: "Waiting for the backend Deployment to create a serving Pod",
	}
}

func hasActiveBackendPod(pods []corev1.Pod) bool {
	for i := range pods {
		if activeBackendPod(&pods[i]) {
			return true
		}
	}
	return false
}

func activeBackendPod(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp == nil &&
		pod.Status.Phase != corev1.PodSucceeded &&
		pod.Status.Phase != corev1.PodFailed
}

func observeBackendPod(pod *corev1.Pod) backendObservation {
	container := servingContainerStatus(pod)
	if container != nil {
		if imagePullFailed(container.State.Waiting) {
			return backendObservation{
				reason:   reasonImagePullFailed,
				message:  fmt.Sprintf("Backend Pod %q cannot pull its serving image; check the image and imagePullSecrets", pod.Name),
				degraded: true,
			}
		}
		if terminatedByOOM(container) {
			return backendObservation{
				reason:   reasonOOMKilled,
				message:  fmt.Sprintf("Backend Pod %q was OOMKilled; inspect its memory limit and model runtime requirements", pod.Name),
				degraded: true,
			}
		}
		if container.State.Waiting != nil && container.State.Waiting.Reason == reasonCrashLoopBackOff {
			return backendObservation{
				reason:   reasonCrashLoopBackOff,
				message:  fmt.Sprintf("Backend Pod %q is in CrashLoopBackOff; inspect its serving-container logs and configuration", pod.Name),
				degraded: true,
			}
		}
		if container.RestartCount >= repeatedRestartThreshold {
			return backendObservation{
				reason:   reasonContainerRestarting,
				message:  fmt.Sprintf("Backend Pod %q repeatedly restarted; inspect its serving-container logs and exit reason", pod.Name),
				degraded: true,
			}
		}
	}

	if podSchedulingDelayed(pod) {
		return backendObservation{
			reason:  reasonSchedulingDelayed,
			message: fmt.Sprintf("Backend Pod %q is unschedulable; inspect accelerator capacity, selectors, tolerations, and scheduler events", pod.Name),
		}
	}
	if container != nil && container.State.Running != nil && !container.Ready {
		return backendObservation{
			reason:  reasonModelLoading,
			message: fmt.Sprintf("Backend Pod %q is running while the model loads and readiness checks complete", pod.Name),
		}
	}
	return backendObservation{
		reason:  "Starting",
		message: fmt.Sprintf("Backend Pod %q is starting", pod.Name),
	}
}

func deploymentProgressDeadlineExceeded(dep *appsv1.Deployment) bool {
	for i := range dep.Status.Conditions {
		condition := &dep.Status.Conditions[i]
		if condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == reasonProgressDeadlineExceeded {
			return true
		}
	}
	return false
}

func servingContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == backendruntime.ServingContainerName {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

func imagePullFailed(waiting *corev1.ContainerStateWaiting) bool {
	if waiting == nil {
		return false
	}
	switch waiting.Reason {
	case "ErrImagePull", reasonImagePullBackOff, "InvalidImageName", "RegistryUnavailable":
		return true
	default:
		return false
	}
}

func terminatedByOOM(container *corev1.ContainerStatus) bool {
	return container.State.Terminated != nil && container.State.Terminated.Reason == reasonOOMKilled ||
		container.LastTerminationState.Terminated != nil && container.LastTerminationState.Terminated.Reason == reasonOOMKilled
}

func podSchedulingDelayed(pod *corev1.Pod) bool {
	for i := range pod.Status.Conditions {
		condition := &pod.Status.Conditions[i]
		if condition.Type == corev1.PodScheduled &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}
	return false
}

func observationPriority(reason string) int {
	switch reason {
	case reasonImagePullFailed:
		return 0
	case reasonOOMKilled:
		return 1
	case reasonCrashLoopBackOff:
		return 2
	case reasonContainerRestarting:
		return 3
	case reasonSchedulingDelayed:
		return 4
	case reasonModelLoading:
		return 5
	default:
		return 6
	}
}

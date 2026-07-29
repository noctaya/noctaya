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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObserveBackend(t *testing.T) {
	now := metav1.NewTime(time.Now())
	cases := []struct {
		name     string
		dep      appsv1.Deployment
		pods     []corev1.Pod
		reason   string
		degraded bool
	}{
		{
			name: "scheduling delay",
			pods: []corev1.Pod{backendPodStatus("pending", corev1.PodPending, corev1.ContainerStatus{}, []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
			}})},
			reason: reasonSchedulingDelayed,
		},
		{
			name: "image pull failure",
			pods: []corev1.Pod{backendPodStatus("image", corev1.PodPending, corev1.ContainerStatus{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonImagePullBackOff}},
			}, nil)},
			reason:   reasonImagePullFailed,
			degraded: true,
		},
		{
			name: "oom killed",
			pods: []corev1.Pod{backendPodStatus("oom", corev1.PodRunning, corev1.ContainerStatus{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonCrashLoopBackOff}},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Reason: reasonOOMKilled},
				},
			}, nil)},
			reason:   reasonOOMKilled,
			degraded: true,
		},
		{
			name: "crash loop",
			pods: []corev1.Pod{backendPodStatus("crash", corev1.PodRunning, corev1.ContainerStatus{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonCrashLoopBackOff}},
			}, nil)},
			reason:   reasonCrashLoopBackOff,
			degraded: true,
		},
		{
			name: "repeated termination",
			pods: []corev1.Pod{backendPodStatus("restarting", corev1.PodRunning, corev1.ContainerStatus{
				RestartCount: repeatedRestartThreshold,
				State:        corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "Error"}},
			}, nil)},
			reason:   reasonContainerRestarting,
			degraded: true,
		},
		{
			name: "deployment progress deadline",
			dep: appsv1.Deployment{Status: appsv1.DeploymentStatus{
				Replicas: 1,
				Conditions: []appsv1.DeploymentCondition{{
					Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: reasonProgressDeadlineExceeded,
				}},
			}},
			reason:   reasonProgressDeadlineExceeded,
			degraded: true,
		},
		{
			name: "ordinary model loading",
			pods: []corev1.Pod{backendPodStatus("loading", corev1.PodRunning, corev1.ContainerStatus{
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}, nil)},
			reason: reasonModelLoading,
		},
		{
			name: "terminating failure is stale",
			pods: []corev1.Pod{
				func() corev1.Pod {
					pod := backendPodStatus("stale", corev1.PodPending, corev1.ContainerStatus{
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonImagePullBackOff}},
					}, nil)
					pod.DeletionTimestamp = &now
					return pod
				}(),
				backendPodStatus("loading", corev1.PodRunning, corev1.ContainerStatus{
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				}, nil),
			},
			reason: reasonModelLoading,
		},
		{
			name: "failure priority is deterministic",
			pods: []corev1.Pod{
				backendPodStatus("crash", corev1.PodRunning, corev1.ContainerStatus{
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reasonCrashLoopBackOff}},
				}, nil),
				backendPodStatus("image", corev1.PodPending, corev1.ContainerStatus{
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"}},
				}, nil),
			},
			reason:   reasonImagePullFailed,
			degraded: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := observeBackend(&tc.dep, tc.pods)
			if got.reason != tc.reason {
				t.Fatalf("reason = %q, want %q", got.reason, tc.reason)
			}
			if got.degraded != tc.degraded {
				t.Fatalf("degraded = %t, want %t", got.degraded, tc.degraded)
			}
		})
	}
}

func backendPodStatus(
	name string,
	phase corev1.PodPhase,
	container corev1.ContainerStatus,
	conditions []corev1.PodCondition,
) corev1.Pod {
	container.Name = "serving"
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.PodStatus{
			Phase:             phase,
			Conditions:        conditions,
			ContainerStatuses: []corev1.ContainerStatus{container},
		},
	}
}

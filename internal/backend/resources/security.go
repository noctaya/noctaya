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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	"github.com/noctaya/noctaya/internal/gateway/proxy"
)

const (
	clientAuthVolumeName = "client-auth"
	clientAuthMountPath  = "/var/run/noctaya/client-auth"
	clientAPIKeyPath     = clientAuthMountPath + "/api-key"
)

func disableServiceAccountToken(pod *corev1.PodSpec) {
	pod.AutomountServiceAccountToken = ptr.To(false)
}

func hardenNoctayaContainer(pod *corev1.PodSpec, container *corev1.Container) {
	uid := int64(65532)
	disableServiceAccountToken(pod)
	pod.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		RunAsUser:    &uid,
		RunAsGroup:   &uid,
		FSGroup:      &uid,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	container.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func configureClientAuthentication(
	pod *corev1.PodSpec,
	container *corev1.Container,
	svc *servingv1alpha1.LLMService,
) {
	authentication := svc.Spec.Endpoint.Authentication
	if authentication == nil {
		return
	}
	mode := int32(0o440)
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: clientAuthVolumeName,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName:  authentication.SecretRef.Name,
			DefaultMode: &mode,
			Items: []corev1.KeyToPath{{
				Key: authentication.SecretRef.Key, Path: "api-key",
			}},
		}},
	})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name: clientAuthVolumeName, MountPath: clientAuthMountPath, ReadOnly: true,
	})
	container.Env = append(container.Env, corev1.EnvVar{
		Name: proxy.EnvClientAPIKeyFile, Value: clientAPIKeyPath,
	})
}

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

package backend

import (
	"fmt"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
)

const (
	CacheMountPath = "/cache"

	cacheVolumeName                  = "model-cache"
	defaultCacheSize                 = "50Gi"
	torchDeviceBackendAutoloadEnvVar = "TORCH_DEVICE_BACKEND_AUTOLOAD"
)

func CachePVCName(svc *servingv1alpha1.LLMService) string { return svc.Name + "-cache" }

func PrewarmJobName(svc *servingv1alpha1.LLMService) string { return svc.Name + "-prewarm" }

// cacheEnv points HuggingFace and ModelScope at the mounted cache so a cold pod loads
// weights from local disk instead of re-downloading them.
func cacheEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "HF_HOME", Value: CacheMountPath + "/hf"},
		{Name: "MODELSCOPE_CACHE", Value: CacheMountPath + "/modelscope"},
	}
}

type cacheArtifacts struct {
	volume *corev1.Volume
	mount  *corev1.VolumeMount
	env    []corev1.EnvVar
	pvc    *corev1.PersistentVolumeClaim
}

func planCache(svc *servingv1alpha1.LLMService) (cacheArtifacts, error) {
	strategy := svc.Spec.Cache.Strategy
	if strategy == "" {
		strategy = "NodeLocalPVC"
	}

	switch strategy {
	case "None":
		return cacheArtifacts{}, nil

	case "BakedImage":
		return cacheArtifacts{}, fmt.Errorf("cache strategy %q is not supported yet", strategy)

	case "HostPath":
		hostType := corev1.HostPathDirectoryOrCreate
		path := fmt.Sprintf("/var/lib/noctaya/cache/%s-%s", svc.Namespace, svc.Name)
		return cacheArtifacts{
			volume: &corev1.Volume{Name: cacheVolumeName, VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: path, Type: &hostType},
			}},
			mount: &corev1.VolumeMount{Name: cacheVolumeName, MountPath: CacheMountPath},
			env:   cacheEnv(),
		}, nil

	case "NodeLocalPVC":
		size := resource.MustParse(defaultCacheSize)
		if svc.Spec.Cache.Size != nil {
			size = *svc.Spec.Cache.Size
		}
		pvc := &corev1.PersistentVolumeClaim{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
			ObjectMeta: metav1.ObjectMeta{Name: CachePVCName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
			},
		}
		if sc := svc.Spec.Cache.StorageClassName; sc != nil && *sc != "" {
			pvc.Spec.StorageClassName = sc
		}
		return cacheArtifacts{
			volume: &corev1.Volume{Name: cacheVolumeName, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: CachePVCName(svc)},
			}},
			mount: &corev1.VolumeMount{Name: cacheVolumeName, MountPath: CacheMountPath},
			env:   cacheEnv(),
			pvc:   pvc,
		}, nil

	default:
		return cacheArtifacts{}, fmt.Errorf("cache strategy %q is not supported in v0", strategy)
	}
}

func applyCache(pod *corev1.PodSpec, art cacheArtifacts) {
	if art.volume == nil {
		return
	}
	pod.Volumes = append(pod.Volumes, *art.volume)
	for i := range pod.Containers {
		if pod.Containers[i].Name != ServingContainerName {
			continue
		}
		pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, *art.mount)
		pod.Containers[i].Env = append(pod.Containers[i].Env, art.env...)
	}
}

func BuildCachePVC(svc *servingv1alpha1.LLMService) (*corev1.PersistentVolumeClaim, error) {
	art, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	return art.pvc, nil
}

// BuildPrewarmJob renders a one-time Job that downloads weights into the cache. It returns
// nil when prewarm is off or the strategy keeps no persistent cache. The command assumes
// huggingface_hub / modelscope are present in the runtime image (validated at run time).
func BuildPrewarmJob(svc *servingv1alpha1.LLMService, rt *servingv1alpha1.InferenceRuntime, m ResolvedModel) (*batchv1.Job, error) {
	// pvc:// weights are already on disk; there is nothing to prewarm.
	if m.Source == "pvc" {
		return nil, nil
	}
	if !svc.Spec.Cache.Prewarm {
		return nil, nil
	}
	art, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	if art.volume == nil {
		return nil, nil
	}

	pod := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyOnFailure,
		NodeSelector:  maps.Clone(rt.Spec.Accelerator.NodeSelector),
		Tolerations:   append([]corev1.Toleration(nil), rt.Spec.Accelerator.Tolerations...),
		SchedulerName: rt.Spec.Accelerator.Scheduler.Name,
		Containers: []corev1.Container{{
			Name:    "prewarm",
			Image:   rt.Spec.Container.Image,
			Command: downloadCommand(m),
			// Prewarming only downloads files. Disable PyTorch's device-backend discovery so
			// vendor packages cannot require host driver libraries in this accelerator-free Pod.
			Env: append(append(cacheEnv(), corev1.EnvVar{
				Name:  torchDeviceBackendAutoloadEnvVar,
				Value: "0",
			}), m.Env...),
			VolumeMounts: []corev1.VolumeMount{*art.mount},
		}},
		Volumes: []corev1.Volume{*art.volume},
	}
	applyImagePullSecrets(&pod, svc)

	backoff := int32(4)
	templateMetadata := metav1.ObjectMeta{Labels: SelectorLabels(svc)}
	if queue := rt.Spec.Accelerator.Scheduler.Queue; queue != "" {
		templateMetadata.Annotations = map[string]string{volcanoQueueAnnotation: queue}
	}
	return &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: PrewarmJobName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: templateMetadata,
				Spec:       pod,
			},
		},
	}, nil
}

func downloadCommand(m ResolvedModel) []string {
	if m.Source == "modelscope" {
		return []string{"python3", "-c", fmt.Sprintf("from modelscope import snapshot_download; snapshot_download(%q)", m.Path)}
	}
	return []string{"python3", "-c", fmt.Sprintf("from huggingface_hub import snapshot_download; snapshot_download(%q)", m.Path)}
}

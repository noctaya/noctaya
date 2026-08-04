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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/noctaya/noctaya/api/v1alpha1"
	backendruntime "github.com/noctaya/noctaya/internal/backend/runtime"
)

const (
	CacheMountPath = "/cache"
	// CreateOnceHashAnnotation records the immutable desired specification of cache resources.
	CreateOnceHashAnnotation = "serving.noctaya.io/create-once-hash"

	cacheVolumeName                  = "model-cache"
	defaultCacheSize                 = "50Gi"
	sharedCacheReadyPath             = CacheMountPath + "/.noctaya-ready"
	torchDeviceBackendAutoloadEnvVar = "TORCH_DEVICE_BACKEND_AUTOLOAD"
	ociPullerImage                   = "ghcr.io/oras-project/oras:v1.3.0"
	ociStagingVolumeName             = "oci-staging"
	ociStagingPath                   = "/staging/model"
	ociAuthVolumeName                = "oci-registry-auth"
	ociAuthMountPath                 = "/var/run/noctaya/registry-auth"
	cacheStrategySharedPVC           = "SharedPVC"
	modelSourceOCI                   = "oci"
	pythonExecutable                 = "python3"
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

	case "NodeLocalPVC", cacheStrategySharedPVC:
		size := resource.MustParse(defaultCacheSize)
		if svc.Spec.Cache.Size != nil {
			size = *svc.Spec.Cache.Size
		}
		accessMode := corev1.ReadWriteOnce
		readOnly := false
		if strategy == cacheStrategySharedPVC {
			accessMode = corev1.ReadWriteMany
			readOnly = svc.Spec.Cache.Prewarm
		}
		pvc := &corev1.PersistentVolumeClaim{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
			ObjectMeta: metav1.ObjectMeta{Name: CachePVCName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{accessMode},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
			},
		}
		if sc := svc.Spec.Cache.StorageClassName; sc != nil && *sc != "" {
			pvc.Spec.StorageClassName = sc
		}
		annotateCreateOnce(pvc, pvc.Spec)
		return cacheArtifacts{
			volume: &corev1.Volume{Name: cacheVolumeName, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: CachePVCName(svc), ReadOnly: readOnly,
				},
			}},
			mount: &corev1.VolumeMount{Name: cacheVolumeName, MountPath: CacheMountPath, ReadOnly: readOnly},
			env:   cacheEnv(),
			pvc:   pvc,
		}, nil

	default:
		return cacheArtifacts{}, fmt.Errorf("cache strategy %q is not supported in v0", strategy)
	}
}

func applyCache(
	pod *corev1.PodSpec,
	art cacheArtifacts,
	svc *servingv1alpha1.LLMService,
	model backendruntime.ResolvedModel,
) {
	if art.volume == nil {
		return
	}
	volume := art.volume.DeepCopy()
	mount := art.mount.DeepCopy()
	if model.Source == modelSourceOCI {
		mount.ReadOnly = true
		if volume.PersistentVolumeClaim != nil {
			volume.PersistentVolumeClaim.ReadOnly = true
		}
	}
	pod.Volumes = append(pod.Volumes, *volume)
	for i := range pod.Containers {
		if pod.Containers[i].Name != backendruntime.ServingContainerName {
			continue
		}
		pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, *mount)
		pod.Containers[i].Env = append(pod.Containers[i].Env, art.env...)
	}
	readyPath := ""
	if model.Source == modelSourceOCI {
		readyPath = model.ReadyPath
	} else if svc.Spec.Cache.Strategy == cacheStrategySharedPVC && svc.Spec.Cache.Prewarm {
		readyPath = sharedCacheReadyPath
	}
	if readyPath != "" {
		pod.InitContainers = append(pod.InitContainers, corev1.Container{
			Name:  "wait-for-prewarm",
			Image: servingContainerImage(pod),
			Command: []string{pythonExecutable, "-c", fmt.Sprintf(
				"import os,time\nwhile not os.path.isfile(%q): time.sleep(2)", readyPath,
			)},
			VolumeMounts: []corev1.VolumeMount{{
				Name: cacheVolumeName, MountPath: CacheMountPath, ReadOnly: true,
			}},
		})
	}
}

func servingContainerImage(pod *corev1.PodSpec) string {
	for i := range pod.Containers {
		if pod.Containers[i].Name == backendruntime.ServingContainerName {
			return pod.Containers[i].Image
		}
	}
	return ""
}

func BuildCachePVC(svc *servingv1alpha1.LLMService) (*corev1.PersistentVolumeClaim, error) {
	art, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	return art.pvc, nil
}

// BuildPrewarmJob renders a one-time Job that downloads weights into the cache. It returns
// nil when prewarm is off or the strategy keeps no persistent cache. OCI sources always
// stage through this Job and require persistent storage.
func BuildPrewarmJob(
	svc *servingv1alpha1.LLMService,
	rt *servingv1alpha1.InferenceRuntime,
	m backendruntime.ResolvedModel,
) (*batchv1.Job, error) {
	// pvc:// weights are already on disk; there is nothing to prewarm.
	if m.Source == "pvc" {
		return nil, nil
	}
	if !svc.Spec.Cache.Prewarm && m.Source != modelSourceOCI {
		return nil, nil
	}
	art, err := planCache(svc)
	if err != nil {
		return nil, err
	}
	if art.volume == nil {
		if m.Source == modelSourceOCI {
			return nil, fmt.Errorf("oci:// model sources require a persistent cache strategy")
		}
		return nil, nil
	}
	prewarmVolume := art.volume.DeepCopy()
	prewarmMount := art.mount.DeepCopy()
	if prewarmVolume.PersistentVolumeClaim != nil {
		prewarmVolume.PersistentVolumeClaim.ReadOnly = false
	}
	prewarmMount.ReadOnly = false

	pod := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyOnFailure,
		NodeSelector:  maps.Clone(rt.Spec.Accelerator.NodeSelector),
		Tolerations:   append([]corev1.Toleration(nil), rt.Spec.Accelerator.Tolerations...),
		SchedulerName: rt.Spec.Accelerator.Scheduler.Name,
		Containers: []corev1.Container{{
			Name:    "prewarm",
			Image:   rt.Spec.Container.Image,
			Command: downloadCommand(m, svc.Spec.Cache.Strategy == cacheStrategySharedPVC),
			// Prewarming only downloads files. Disable PyTorch's device-backend discovery so
			// vendor packages cannot require host driver libraries in this accelerator-free Pod.
			Env: append(append(cacheEnv(), corev1.EnvVar{
				Name:  torchDeviceBackendAutoloadEnvVar,
				Value: "0",
			}), m.Env...),
			VolumeMounts: []corev1.VolumeMount{*prewarmMount},
		}},
		Volumes: []corev1.Volume{*prewarmVolume},
	}
	if m.Source == modelSourceOCI {
		configureOCIModelPull(&pod, m)
	}
	disableServiceAccountToken(&pod)
	applyImagePullSecrets(&pod, svc)

	backoff := int32(4)
	templateMetadata := metav1.ObjectMeta{Labels: SelectorLabels(svc)}
	if queue := rt.Spec.Accelerator.Scheduler.Queue; queue != "" {
		templateMetadata.Annotations = map[string]string{volcanoQueueAnnotation: queue}
	}
	job := &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: PrewarmJobName(svc), Namespace: svc.Namespace, Labels: SelectorLabels(svc)},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: templateMetadata,
				Spec:       pod,
			},
		},
	}
	annotateCreateOnce(job, job.Spec)
	return job, nil
}

func downloadCommand(m backendruntime.ResolvedModel, markReady bool) []string {
	ready := ""
	if markReady {
		ready = fmt.Sprintf("; from pathlib import Path; Path(%q).touch()", sharedCacheReadyPath)
	}
	if m.Source == "modelscope" {
		return []string{pythonExecutable, "-c", fmt.Sprintf("from modelscope import snapshot_download; snapshot_download(%q)%s", m.Path, ready)}
	}
	return []string{pythonExecutable, "-c", fmt.Sprintf("from huggingface_hub import snapshot_download; snapshot_download(%q)%s", m.Path, ready)}
}

func configureOCIModelPull(pod *corev1.PodSpec, model backendruntime.ResolvedModel) {
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name:         ociStagingVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	puller := corev1.Container{
		Name:  "pull-oci-model",
		Image: ociPullerImage,
		Args: []string{
			"pull", model.OCIReference, "--output", ociStagingPath, "--no-tty",
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name: ociStagingVolumeName, MountPath: ociStagingPath,
		}},
	}
	if model.OCISecretName != "" {
		mode := int32(0o400)
		pod.Volumes = append(pod.Volumes, corev1.Volume{
			Name: ociAuthVolumeName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  model.OCISecretName,
				DefaultMode: &mode,
				Items: []corev1.KeyToPath{{
					Key: corev1.DockerConfigJsonKey, Path: "config.json",
				}},
			}},
		})
		puller.VolumeMounts = append(puller.VolumeMounts, corev1.VolumeMount{
			Name: ociAuthVolumeName, MountPath: ociAuthMountPath, ReadOnly: true,
		})
		puller.Args = append(puller.Args, "--registry-config", ociAuthMountPath+"/config.json")
	}
	pod.InitContainers = append(pod.InitContainers, puller)
	pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name: ociStagingVolumeName, MountPath: ociStagingPath, ReadOnly: true,
	})
	pod.Containers[0].Command = []string{pythonExecutable, "-c", ociPromotionScript(model)}
}

func ociPromotionScript(model backendruntime.ResolvedModel) string {
	return fmt.Sprintf(`import os, pathlib, shutil
src = %q
dst = %q
partial = dst + ".partial"
ready = %q
if os.path.isfile(ready):
    raise SystemExit(0)
for root, dirs, files in os.walk(src):
    for name in dirs + files:
        if os.path.islink(os.path.join(root, name)):
            raise RuntimeError("OCI model artifacts must not contain symbolic links")
pathlib.Path(os.path.dirname(dst)).mkdir(parents=True, exist_ok=True)
shutil.rmtree(partial, ignore_errors=True)
shutil.copytree(src, partial)
shutil.rmtree(dst, ignore_errors=True)
os.replace(partial, dst)
pathlib.Path(ready).touch()
`, ociStagingPath, model.Path, model.ReadyPath)
}

func annotateCreateOnce(object metav1.Object, spec any) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		panic(fmt.Sprintf("marshal create-once resource: %v", err))
	}
	digest := sha256.Sum256(encoded)
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[CreateOnceHashAnnotation] = hex.EncodeToString(digest[:])
	object.SetAnnotations(annotations)
}

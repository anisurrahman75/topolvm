package executor

import (
	"context"
	"fmt"
	"os"

	topolvmv1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/mounter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RestoreExecutor handles the creation of restore pods that perform online restores
// of logical volumes from snapshots. It copies the spec from the DaemonSet pod template
// and injects a specialized restore container.
type RestoreExecutor struct {
	client        client.Client
	logicalVolume *topolvmv1.LogicalVolume
	snapshotID    string
	repositoryURL string
	mountResponse *mounter.MountResponse
}

// NewRestoreExecutor creates a new RestoreExecutor instance with the provided dependencies.
func NewRestoreExecutor(
	client client.Client,
	logicalVolume *topolvmv1.LogicalVolume,
	snapshotID string,
	repositoryURL string,
) *RestoreExecutor {
	return &RestoreExecutor{
		client:        client,
		logicalVolume: logicalVolume,
		snapshotID:    snapshotID,
		repositoryURL: repositoryURL,
	}
}

// Execute creates a restore pod that will perform the online restore operation.
func (e *RestoreExecutor) Execute() error {
	objMeta := e.buildObjectMeta()
	hostPod, err := e.getHostPod()
	if err != nil {
		return err
	}

	podSpec, err := e.buildPodSpec(hostPod)
	if err != nil {
		return err
	}

	pod := &corev1.Pod{
		ObjectMeta: objMeta,
		Spec:       podSpec,
	}

	if err := e.client.Create(context.Background(), pod); err != nil {
		return fmt.Errorf("failed to create restore pod: %w", err)
	}
	return nil
}

// buildPodSpec constructs the pod spec by copying from the DaemonSet's pod template
// and replacing containers with the restore container.
func (e *RestoreExecutor) buildPodSpec(hostPod *corev1.Pod) (corev1.PodSpec, error) {
	daemonSet, err := e.getDaemonSetFromOwnerRef(hostPod)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	var templateContainer corev1.Container
	if len(daemonSet.Spec.Template.Spec.Containers) > 0 {
		templateContainer = daemonSet.Spec.Template.Spec.Containers[0]
	}

	restoreContainer := e.buildRestoreContainer(&templateContainer)

	// Copy the entire pod spec from DaemonSet template but replace containers with restore container
	podSpec := daemonSet.Spec.Template.Spec.DeepCopy()
	podSpec.Containers = []corev1.Container{restoreContainer}
	podSpec.RestartPolicy = corev1.RestartPolicyNever

	// Override affinity from the actual running pod
	if hostPod.Spec.Affinity != nil {
		podSpec.Affinity = hostPod.Spec.Affinity.DeepCopy()
	}

	return *podSpec, nil
}

// getHostPod retrieves the current pod running this executor.
func (e *RestoreExecutor) getHostPod() (*corev1.Pod, error) {
	hostPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      os.Getenv(EnvHostName),
			Namespace: os.Getenv(EnvHostNamespace),
		},
	}

	if err := e.client.Get(context.Background(), client.ObjectKeyFromObject(hostPod), hostPod); err != nil {
		return nil, fmt.Errorf("failed to get host pod: %w", err)
	}

	return hostPod, nil
}

// getDaemonSetFromOwnerRef retrieves the DaemonSet from the pod's owner reference.
func (e *RestoreExecutor) getDaemonSetFromOwnerRef(pod *corev1.Pod) (*appsv1.DaemonSet, error) {
	var daemonSetRef *metav1.OwnerReference
	for i := range pod.OwnerReferences {
		if pod.OwnerReferences[i].Kind == "DaemonSet" && pod.OwnerReferences[i].APIVersion == "apps/v1" {
			daemonSetRef = &pod.OwnerReferences[i]
			break
		}
	}

	if daemonSetRef == nil {
		return nil, fmt.Errorf("pod %s/%s does not have a DaemonSet owner reference", pod.Namespace, pod.Name)
	}

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      daemonSetRef.Name,
			Namespace: pod.Namespace,
		},
	}

	if err := e.client.Get(context.Background(), client.ObjectKeyFromObject(daemonSet), daemonSet); err != nil {
		return nil, fmt.Errorf("failed to get DaemonSet %s/%s: %w", pod.Namespace, daemonSetRef.Name, err)
	}

	return daemonSet, nil
}

// buildRestoreContainer creates a container configured to execute the online restore command.
func (e *RestoreExecutor) buildRestoreContainer(templateContainer *corev1.Container) corev1.Container {
	image := templateContainer.Image
	imagePullPolicy := corev1.PullIfNotPresent
	if templateContainer.ImagePullPolicy != "" {
		imagePullPolicy = templateContainer.ImagePullPolicy
	}

	var volumeMounts []corev1.VolumeMount
	volumeMounts = templateContainer.VolumeMounts

	container := corev1.Container{
		Name:            RestoreContainerName,
		Image:           image,
		ImagePullPolicy: imagePullPolicy,
		Command: []string{
			"/online-snapshotter",
			RestoreCommandName, // "restore" subcommand
		},
		Args:            e.buildRestoreArgs(),
		Env:             e.buildRestoreEnv(templateContainer),
		VolumeMounts:    volumeMounts,
		SecurityContext: e.buildSecurityContext(),
		Resources:       e.buildResourceRequirements(),
	}

	return container
}

// buildRestoreArgs constructs the command-line arguments for the restore command.
func (e *RestoreExecutor) buildRestoreArgs() []string {
	args := []string{
		fmt.Sprintf("--logical-volume=%s", e.logicalVolume.Name),
		fmt.Sprintf("--node-name=%s", e.logicalVolume.Spec.NodeName),
		fmt.Sprintf("--device-class=%s", e.logicalVolume.Spec.DeviceClass),
		fmt.Sprintf("--mount-path=%s", e.mountResponse.MountPath),
		fmt.Sprintf("--snapshot-id=%s", e.snapshotID),
	}

	if e.repositoryURL != "" {
		args = append(args, fmt.Sprintf("--repository-url=%s", e.repositoryURL))
	}

	return args
}

// buildRestoreEnv constructs the environment variables for the restore container.
func (e *RestoreExecutor) buildRestoreEnv(templateContainer *corev1.Container) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name:  "LOGICAL_VOLUME_NAME",
			Value: e.logicalVolume.Name,
		},
		{
			Name:  "LOGICAL_VOLUME_SIZE",
			Value: e.logicalVolume.Spec.Size.String(),
		},
		{
			Name:  "NODE_NAME",
			Value: e.logicalVolume.Spec.NodeName,
		},
		{
			Name:  "DEVICE_CLASS",
			Value: e.logicalVolume.Spec.DeviceClass,
		},
		{
			Name:  "RESTORE_SNAPSHOT_ID",
			Value: e.snapshotID,
		},
		{
			Name:  "RESTORE_REPOSITORY_URL",
			Value: e.repositoryURL,
		},
	}

	// Copy non-conflicting environment variables from template container
	if templateContainer != nil {
		for _, envVar := range templateContainer.Env {
			if !e.isReservedEnvVar(envVar.Name) {
				env = append(env, envVar)
			}
		}
	}

	return env
}

// isReservedEnvVar checks if an environment variable name is reserved for restore operations.
func (e *RestoreExecutor) isReservedEnvVar(name string) bool {
	reserved := []string{
		"LOGICAL_VOLUME_NAME",
		"LOGICAL_VOLUME_SIZE",
		"NODE_NAME",
		"DEVICE_CLASS",
		"RESTORE_SNAPSHOT_ID",
		"RESTORE_REPOSITORY_URL",
	}

	for _, r := range reserved {
		if name == r {
			return true
		}
	}
	return false
}

// buildSecurityContext creates the security context for the restore container.
func (e *RestoreExecutor) buildSecurityContext() *corev1.SecurityContext {
	privileged := true
	return &corev1.SecurityContext{
		Privileged: &privileged,
	}
}

// buildResourceRequirements creates the resource requirements for the restore container.
func (e *RestoreExecutor) buildResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
}

// buildObjectMeta creates the metadata for the restore pod.
func (e *RestoreExecutor) buildObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      fmt.Sprintf("%s-restore", e.logicalVolume.Name),
		Namespace: e.logicalVolume.Namespace,
		Labels: map[string]string{
			LabelAppKey:           LabelAppValue,
			LabelLogicalVolumeKey: e.logicalVolume.Name,
			LabelRestorePodKey:    "true",
		},
		Annotations: map[string]string{
			"topolvm.io/snapshot-id":    e.snapshotID,
			"topolvm.io/repository-url": e.repositoryURL,
		},
	}
}

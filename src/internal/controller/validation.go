package controller

import (
	"errors"
	"fmt"
	"strings"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	corev1 "k8s.io/api/core/v1"
	kvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	maxRanks              = 64
	maxDevicesPerWorkload = 128
	maxContainers         = 16
)

// validateWorkload rejects unsafe templates and requests outside production limits.
func validateWorkload(workload *ttapi.Workload) error {
	if workload == nil {
		return errors.New("workload is nil")
	}
	if len(workload.Spec.Ranks) == 0 || len(workload.Spec.Ranks) > maxRanks {
		return fmt.Errorf("rank count must be between 1 and %d", maxRanks)
	}
	seen, total := map[string]struct{}{}, int64(0)
	for _, rank := range workload.Spec.Ranks {
		if problems := kvalidation.IsDNS1123Label(rank.Name); len(problems) > 0 {
			return fmt.Errorf("rank %q is not DNS-safe", rank.Name)
		}
		if _, duplicate := seen[rank.Name]; duplicate {
			return fmt.Errorf("rank %q is duplicated", rank.Name)
		}
		seen[rank.Name] = struct{}{}
		if (!dra.MatchesDeviceClass(rank.DeviceClassName, rank.ChipSeries) && rank.ChipSeries != "") ||
			!dra.SupportedDeviceClass(rank.DeviceClassName) {
			return fmt.Errorf("rank %q has an unsupported DeviceClass or chip series", rank.Name)
		}
		if rank.Count < 1 {
			return fmt.Errorf("rank %q count must be positive", rank.Name)
		}
		total += rank.Count
	}
	if total > maxDevicesPerWorkload {
		return fmt.Errorf("workload requests %d devices; maximum is %d", total, maxDevicesPerWorkload)
	}
	for name, value := range workload.Spec.PodTemplate.Annotations {
		if strings.HasPrefix(name, "container.apparmor.security.beta.kubernetes.io/") && value == "unconfined" {
			return errors.New("Pod template may not disable AppArmor")
		}
	}
	return validatePodTemplate(workload.Spec.ContainerName, &workload.Spec.PodTemplate.Spec)
}

// validatePodTemplate prevents controller-managed fields and privileged Pod settings.
func validatePodTemplate(target string, spec *corev1.PodSpec) error {
	if spec.RestartPolicy != corev1.RestartPolicyNever {
		return errors.New("Pod template restartPolicy must be Never")
	}
	if spec.NodeName != "" || spec.NodeSelector[corev1.LabelHostname] != "" || len(spec.ResourceClaims) != 0 {
		return errors.New("Pod template sets controller-managed placement or claims")
	}
	if spec.ServiceAccountName != "" || spec.AutomountServiceAccountToken != nil && *spec.AutomountServiceAccountToken {
		return errors.New("Pod template may not select or mount a ServiceAccount token")
	}
	if spec.HostNetwork || spec.HostPID || spec.HostIPC {
		return errors.New("Pod template may not use host namespaces")
	}
	if spec.Affinity != nil {
		return errors.New("Pod template may not set controller-managed node affinity")
	}
	for _, volume := range spec.Volumes {
		if volume.HostPath != nil {
			return errors.New("Pod template may not use hostPath volumes")
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.ServiceAccountToken != nil {
					return errors.New("Pod template may not project ServiceAccount tokens")
				}
			}
		}
	}
	if len(spec.EphemeralContainers) != 0 {
		return errors.New("Pod template may not define ephemeral containers")
	}
	containers := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	if len(spec.Containers) == 0 || len(containers) > maxContainers {
		return fmt.Errorf("Pod template must contain 1 to %d containers", maxContainers)
	}
	foundTarget := false
	for _, container := range spec.Containers {
		if container.Name == target {
			foundTarget = true
			break
		}
	}
	for _, container := range containers {
		if container.SecurityContext != nil && container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
			return fmt.Errorf("container %q requests privileged access", container.Name)
		}
		if len(container.Resources.Claims) != 0 {
			return fmt.Errorf("container %q sets controller-managed DRA claims", container.Name)
		}
		for _, variable := range container.Env {
			if variable.Name == "TT_RANK" || variable.Name == "TT_WORLD_SIZE" {
				return fmt.Errorf("container %q sets controller-managed environment", container.Name)
			}
		}
	}
	if !foundTarget {
		return fmt.Errorf("container %q not found in Pod template", target)
	}
	return nil
}

package test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
)

// safeWorkload returns a minimal workload accepted by production validation.
func safeWorkload() *ttapi.Workload {
	return &ttapi.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "default", UID: types.UID("job-uid")},
		Spec: ttapi.WorkloadSpec{
			ContainerName: "worker",
			PodTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "worker", Image: "example.invalid/worker"}},
			}},
			Ranks: []ttapi.WorkloadRank{{
				Name:            "rank-0",
				DeviceClassName: dra.GenericDeviceClassName,
				Count:           1,
			}},
		},
	}
}

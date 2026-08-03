package api

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	TopologyAPIVersion   = "topology.tenstorrent.com/v1alpha1"
	SchedulingAPIVersion = "scheduling.tenstorrent.com/v1alpha1"
	NodeTopologyKind     = "TenstorrentNodeTopology"
	FabricTopologyKind   = "TenstorrentFabricTopology"
	WorkloadKind         = "TenstorrentWorkload"
)

var (
	NodeTopologyGVR   = schema.GroupVersionResource{Group: "topology.tenstorrent.com", Version: "v1alpha1", Resource: "tenstorrentnodetopologies"}
	FabricTopologyGVR = schema.GroupVersionResource{Group: "topology.tenstorrent.com", Version: "v1alpha1", Resource: "tenstorrentfabrictopologies"}
	WorkloadGVR       = schema.GroupVersionResource{Group: "scheduling.tenstorrent.com", Version: "v1alpha1", Resource: "tenstorrentworkloads"}
)

type TopologyLink struct {
	Name             string `json:"name"`
	State            string `json:"state,omitempty"`
	SpeedGbps        uint64 `json:"speedGbps,omitempty"`
	RemoteEndpointID string `json:"remoteEndpointID,omitempty"`
}
type TopologyDevice struct {
	Pool       string         `json:"pool"`
	Name       string         `json:"name"`
	StableID   string         `json:"stableID"`
	ChipSeries string         `json:"chipSeries"`
	CardSeries string         `json:"cardSeries"`
	FabricID   string         `json:"fabricID,omitempty"`
	RingID     string         `json:"ringID,omitempty"`
	EndpointID string         `json:"endpointID,omitempty"`
	Links      []TopologyLink `json:"links,omitempty"`
}
type NodeTopologySpec struct {
	NodeName   string           `json:"nodeName"`
	ObservedAt metav1.Time      `json:"observedAt"`
	Devices    []TopologyDevice `json:"devices"`
}
type NodeTopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NodeTopologySpec `json:"spec"`
}

type FabricEndpoint struct {
	NodeName   string         `json:"nodeName"`
	Pool       string         `json:"pool"`
	DeviceName string         `json:"deviceName"`
	StableID   string         `json:"stableID"`
	ChipSeries string         `json:"chipSeries"`
	CardSeries string         `json:"cardSeries"`
	FabricID   string         `json:"fabricID"`
	RingID     string         `json:"ringID"`
	EndpointID string         `json:"endpointID"`
	Links      []TopologyLink `json:"links,omitempty"`
}
type FabricTopologyStatus struct {
	Generation string             `json:"generation,omitempty"`
	ObservedAt metav1.Time        `json:"observedAt,omitempty"`
	Valid      bool               `json:"valid"`
	Endpoints  []FabricEndpoint   `json:"endpoints,omitempty"`
	Errors     []string           `json:"errors,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
type FabricTopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Status            FabricTopologyStatus `json:"status,omitempty"`
}

type WorkloadRank struct {
	Name            string `json:"name"`
	DeviceClassName string `json:"deviceClassName"`
	Count           int64  `json:"count,omitempty"`
	ChipSeries      string `json:"chipSeries,omitempty"`
	CardSeries      string `json:"cardSeries,omitempty"`
}
type WorkloadTopology struct {
	FabricID string `json:"fabricID,omitempty"`
	RingID   string `json:"ringID,omitempty"`
}
type WorkloadSpec struct {
	ContainerName string                 `json:"containerName"`
	PodTemplate   corev1.PodTemplateSpec `json:"podTemplate"`
	Topology      WorkloadTopology       `json:"topology,omitempty"`
	Ranks         []WorkloadRank         `json:"ranks"`
}
type AssignedDevice struct {
	Pool       string `json:"pool"`
	Name       string `json:"name"`
	StableID   string `json:"stableID"`
	EndpointID string `json:"endpointID"`
}
type RankAssignment struct {
	Rank      string           `json:"rank"`
	NodeName  string           `json:"nodeName"`
	ClaimName string           `json:"claimName"`
	PodName   string           `json:"podName"`
	Devices   []AssignedDevice `json:"devices"`
}
type WorkloadStatus struct {
	Phase            string             `json:"phase,omitempty"`
	FabricGeneration string             `json:"fabricGeneration,omitempty"`
	Assignments      []RankAssignment   `json:"assignments,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkloadSpec   `json:"spec"`
	Status            WorkloadStatus `json:"status,omitempty"`
}

func ToUnstructured(value any) (*unstructured.Unstructured, error) {
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(value)
	return &unstructured.Unstructured{Object: object}, err
}
func FromUnstructured(value *unstructured.Unstructured, out any) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(value.Object, out)
}

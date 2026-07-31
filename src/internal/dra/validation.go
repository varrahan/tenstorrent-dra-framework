package dra

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/util/errors"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	maxResourceSliceDevices          = 128
	maxDeviceAttributesAndCapacities = 32
	maxResourcePoolNameLength        = 253
	expectedResourceAPIVersion       = "resource.k8s.io/v1"
)

// ValidateDriverName applies the Kubernetes ResourceSlice driver-name schema.
func ValidateDriverName(driverName string) error {
	var errs []error
	if len(driverName) > resourceapi.DriverNameMaxLength {
		errs = append(errs, fmt.Errorf("driver name %q exceeds %d characters", driverName, resourceapi.DriverNameMaxLength))
	}
	for _, detail := range utilvalidation.IsDNS1123Subdomain(driverName) {
		errs = append(errs, fmt.Errorf("invalid driver name %q: %s", driverName, detail))
	}
	return apierrors.NewAggregate(errs)
}

// ValidateDeviceClasses validates the schema used by generated DeviceClasses
// and compiles all CEL selectors in the same boolean-shaped environment used
// by DRA selectors.
func ValidateDeviceClasses(classes []resourceapi.DeviceClass) error {
	var errs []error
	for i := range classes {
		class := &classes[i]
		prefix := fmt.Sprintf("DeviceClass[%d] %q", i, class.Name)
		errs = append(errs, validateTypeMeta(prefix, class.APIVersion, class.Kind, "DeviceClass")...)
		errs = append(errs, validateDNSSubdomain(prefix+" metadata.name", class.Name)...)
		if len(class.Spec.Selectors) == 0 {
			errs = append(errs, fmt.Errorf("%s must define at least one selector", prefix))
		}
		errs = append(errs, validateSelectors(prefix, class.Spec.Selectors)...)
	}
	return apierrors.NewAggregate(errs)
}

// ValidateResourceSlices validates the Kubernetes constraints exercised by the
// generated ResourceSlice model.
func ValidateResourceSlices(slices []resourceapi.ResourceSlice) error {
	var errs []error
	for i := range slices {
		slice := &slices[i]
		prefix := fmt.Sprintf("ResourceSlice[%d] %q", i, slice.Name)
		errs = append(errs, validateTypeMeta(prefix, slice.APIVersion, slice.Kind, "ResourceSlice")...)
		errs = append(errs, validateDNSSubdomain(prefix+" metadata.name", slice.Name)...)
		if err := ValidateDriverName(slice.Spec.Driver); err != nil {
			errs = append(errs, fmt.Errorf("%s spec.driver: %w", prefix, err))
		}
		errs = append(errs, validatePoolName(prefix+" spec.pool.name", slice.Spec.Pool.Name)...)
		if slice.Spec.Pool.ResourceSliceCount < 1 {
			errs = append(errs, fmt.Errorf("%s spec.pool.resourceSliceCount must be greater than zero", prefix))
		}
		nodeSelections := 0
		if slice.Spec.NodeName != nil {
			nodeSelections++
			if *slice.Spec.NodeName == "" {
				errs = append(errs, fmt.Errorf("%s spec.nodeName must not be empty", prefix))
			}
		}
		if slice.Spec.NodeSelector != nil {
			nodeSelections++
		}
		if slice.Spec.AllNodes != nil {
			nodeSelections++
			if !*slice.Spec.AllNodes {
				errs = append(errs, fmt.Errorf("%s spec.allNodes must be true when set", prefix))
			}
		}
		if slice.Spec.PerDeviceNodeSelection != nil {
			nodeSelections++
			if !*slice.Spec.PerDeviceNodeSelection {
				errs = append(errs, fmt.Errorf("%s spec.perDeviceNodeSelection must be true when set", prefix))
			}
		}
		if nodeSelections != 1 {
			errs = append(errs, fmt.Errorf("%s must set exactly one node-selection field", prefix))
		}
		if len(slice.Spec.Devices) > maxResourceSliceDevices {
			errs = append(errs, fmt.Errorf("%s has %d devices, maximum is %d", prefix, len(slice.Spec.Devices), maxResourceSliceDevices))
		}
		for j := range slice.Spec.Devices {
			device := &slice.Spec.Devices[j]
			devicePrefix := fmt.Sprintf("%s device[%d] %q", prefix, j, device.Name)
			errs = append(errs, validateDNSLabel(devicePrefix+" name", device.Name)...)
			if len(device.Attributes)+len(device.Capacity) > maxDeviceAttributesAndCapacities {
				errs = append(errs, fmt.Errorf("%s has %d attributes and capacities, maximum is %d", devicePrefix, len(device.Attributes)+len(device.Capacity), maxDeviceAttributesAndCapacities))
			}
			for name := range device.Attributes {
				errs = append(errs, validateQualifiedName(devicePrefix+" attribute", string(name))...)
			}
			for name := range device.Capacity {
				errs = append(errs, validateQualifiedName(devicePrefix+" capacity", string(name))...)
			}
		}
	}
	return apierrors.NewAggregate(errs)
}

// ValidateResourceClaims validates generated claim request structure and CEL.
func ValidateResourceClaims(claims []resourceapi.ResourceClaim) error {
	var errs []error
	for i := range claims {
		claim := &claims[i]
		prefix := fmt.Sprintf("ResourceClaim[%d] %q", i, claim.Name)
		errs = append(errs, validateTypeMeta(prefix, claim.APIVersion, claim.Kind, "ResourceClaim")...)
		errs = append(errs, validateDNSSubdomain(prefix+" metadata.name", claim.Name)...)
		if len(claim.Spec.Devices.Requests) == 0 {
			errs = append(errs, fmt.Errorf("%s must define at least one device request", prefix))
		}
		for j := range claim.Spec.Devices.Requests {
			request := &claim.Spec.Devices.Requests[j]
			requestPrefix := fmt.Sprintf("%s request[%d] %q", prefix, j, request.Name)
			errs = append(errs, validateDNSLabel(requestPrefix+" name", request.Name)...)
			if request.Exactly == nil {
				errs = append(errs, fmt.Errorf("%s must define exactly", requestPrefix))
				continue
			}
			errs = append(errs, validateDNSSubdomain(requestPrefix+" deviceClassName", request.Exactly.DeviceClassName)...)
			if request.Exactly.Count < 1 {
				errs = append(errs, fmt.Errorf("%s count must be greater than zero", requestPrefix))
			}
			errs = append(errs, validateSelectors(requestPrefix, request.Exactly.Selectors)...)
		}
	}
	return apierrors.NewAggregate(errs)
}

func validateSelectors(prefix string, selectors []resourceapi.DeviceSelector) []error {
	var errs []error
	env, err := cel.NewEnv(cel.Variable("device", cel.DynType))
	if err != nil {
		return []error{fmt.Errorf("create CEL environment: %w", err)}
	}
	for i := range selectors {
		selector := &selectors[i]
		selectorPrefix := fmt.Sprintf("%s selector[%d]", prefix, i)
		if selector.CEL == nil {
			errs = append(errs, fmt.Errorf("%s must define cel", selectorPrefix))
			continue
		}
		if strings.TrimSpace(selector.CEL.Expression) == "" {
			errs = append(errs, fmt.Errorf("%s CEL expression must not be empty", selectorPrefix))
			continue
		}
		ast, issues := env.Compile(selector.CEL.Expression)
		if issues != nil && issues.Err() != nil {
			errs = append(errs, fmt.Errorf("%s CEL expression: %w", selectorPrefix, issues.Err()))
			continue
		}
		if ast.OutputType() != cel.BoolType {
			errs = append(errs, fmt.Errorf("%s CEL expression returns %s, want bool", selectorPrefix, ast.OutputType()))
		}
	}
	return errs
}

func validateTypeMeta(prefix, apiVersion, kind, expectedKind string) []error {
	var errs []error
	if apiVersion != expectedResourceAPIVersion {
		errs = append(errs, fmt.Errorf("%s apiVersion is %q, want %q", prefix, apiVersion, expectedResourceAPIVersion))
	}
	if kind != expectedKind {
		errs = append(errs, fmt.Errorf("%s kind is %q, want %q", prefix, kind, expectedKind))
	}
	return errs
}

func validateDNSSubdomain(field, value string) []error {
	var errs []error
	for _, detail := range utilvalidation.IsDNS1123Subdomain(value) {
		errs = append(errs, fmt.Errorf("%s %q is invalid: %s", field, value, detail))
	}
	return errs
}

func validateDNSLabel(field, value string) []error {
	var errs []error
	for _, detail := range utilvalidation.IsDNS1123Label(value) {
		errs = append(errs, fmt.Errorf("%s %q is invalid: %s", field, value, detail))
	}
	return errs
}

func validateQualifiedName(field, value string) []error {
	var errs []error
	for _, detail := range utilvalidation.IsQualifiedName(value) {
		errs = append(errs, fmt.Errorf("%s %q is invalid: %s", field, value, detail))
	}
	return errs
}

func validatePoolName(field, value string) []error {
	var errs []error
	if len(value) > maxResourcePoolNameLength {
		errs = append(errs, fmt.Errorf("%s exceeds %d characters", field, maxResourcePoolNameLength))
	}
	for _, segment := range strings.Split(value, "/") {
		errs = append(errs, validateDNSSubdomain(field, segment)...)
	}
	return errs
}

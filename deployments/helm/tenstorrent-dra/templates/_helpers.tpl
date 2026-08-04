{{- define "tenstorrent-dra.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tenstorrent-dra.durationMilliseconds" -}}
{{- $value := toString . -}}
{{- $number := int64 (regexFind "^[0-9]+" $value) -}}
{{- if hasSuffix "ms" $value -}}
{{- $number -}}
{{- else if hasSuffix "s" $value -}}
{{- mul $number 1000 -}}
{{- else if hasSuffix "m" $value -}}
{{- mul $number 60000 -}}
{{- else -}}
{{- mul $number 3600000 -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.validate" -}}
{{- if lt (int64 (include "tenstorrent-dra.durationMilliseconds" .Values.inventoryGracePeriod)) (int64 (include "tenstorrent-dra.durationMilliseconds" .Values.interval)) -}}
{{- fail "inventoryGracePeriod must be at least interval" -}}
{{- end -}}
{{- if and .Values.podDisruptionBudget.enabled (gt (int .Values.podDisruptionBudget.minAvailable) (int .Values.controllerReplicas)) -}}
{{- fail "podDisruptionBudget.minAvailable cannot exceed controllerReplicas" -}}
{{- end -}}
{{- if and (not .Values.priorityClass.create) (or (eq .Values.priorityClass.controller.name "") (eq .Values.priorityClass.node.name "")) -}}
{{- fail "priorityClass controller and node names are required when priorityClass.create=false" -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 42 | trimSuffix "-" -}}
{{- else if gt (len .Release.Name) 42 -}}
{{- printf "%s-%s" (.Release.Name | trunc 33 | trimSuffix "-") (.Release.Name | sha256sum | trunc 8) -}}
{{- else -}}
{{- .Release.Name -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "tenstorrent-dra.labels" -}}
helm.sh/chart: {{ include "tenstorrent-dra.chart" . }}
app.kubernetes.io/name: {{ include "tenstorrent-dra.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: tenstorrent-dra
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "tenstorrent-dra.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tenstorrent-dra.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "tenstorrent-dra.componentLabels" -}}
{{ include "tenstorrent-dra.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "tenstorrent-dra.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository .Values.image.tag -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.controllerPriorityClassName" -}}
{{- if .Values.priorityClass.controller.name -}}
{{- .Values.priorityClass.controller.name -}}
{{- else -}}
{{- printf "%s-controller-critical" (include "tenstorrent-dra.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.nodePriorityClassName" -}}
{{- if .Values.priorityClass.node.name -}}
{{- .Values.priorityClass.node.name -}}
{{- else -}}
{{- printf "%s-node-critical" (include "tenstorrent-dra.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "tenstorrent-dra.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "tenstorrent-dra.labels" -}}
app.kubernetes.io/name: {{ include "tenstorrent-dra.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}
{{- define "tenstorrent-dra.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}

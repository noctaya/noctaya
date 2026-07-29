{{- define "noctaya.name" -}}
{{- default .Chart.Name .Values.nameOverride -}}
{{- end -}}

{{- define "noctaya.fullname" -}}
{{- $name := include "noctaya.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "noctaya.tag" -}}
{{- .Values.image.tag | default .Chart.AppVersion -}}
{{- end -}}

{{- define "noctaya.operatorImage" -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.operator (include "noctaya.tag" .) -}}
{{- end -}}

{{- define "noctaya.gatewayImage" -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.gateway (include "noctaya.tag" .) -}}
{{- end -}}

{{- define "noctaya.labels" -}}
app.kubernetes.io/name: {{ include "noctaya.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "noctaya.selectorLabels" -}}
app.kubernetes.io/name: {{ include "noctaya.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: controller-manager
{{- end -}}

{{- define "noctaya.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (printf "%s-controller-manager" (include "noctaya.fullname" .)) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create=false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

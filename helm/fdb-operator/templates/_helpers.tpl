{{/*
Common labels
*/}}
{{- define "chart.labels" -}}
app: fdb-kubernetes-operator-controller-manager
control-plane: controller-manager
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "chart.selectorLabels" -}}
app: fdb-kubernetes-operator-controller-manager
{{- end -}}

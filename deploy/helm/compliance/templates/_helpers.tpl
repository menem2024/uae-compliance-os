{{/*
Shared helpers for the compliance umbrella chart.
Most helpers take a dict: (dict "root" $ "name" "api-go" "component" "api").
*/}}

{{- define "compliance.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Labels on every object and pod the chart owns. */}}
{{- define "compliance.labels" -}}
helm.sh/chart: {{ include "compliance.chart" .root }}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/part-of: {{ .root.Values.partOf }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{- with .component }}
app.kubernetes.io/component: {{ . }}
{{- end }}
{{- end }}

{{- define "compliance.selectorLabels" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end }}

{{/*
Pod-level security context. Every pod runs as a fixed non-root UID.
(dict "uid" 65532) or (dict "uid" 999 "gid" 1000)
*/}}
{{- define "compliance.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .gid | default .uid }}
fsGroup: {{ .gid | default .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
Container-level security context.
(dict "readOnlyRootFilesystem" true)
*/}}
{{- define "compliance.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: {{ .readOnlyRootFilesystem }}
capabilities:
  drop:
    - ALL
{{- end }}

{{/* Locally built service image (imported into k3d, never pulled). */}}
{{- define "compliance.serviceImage" -}}
image: {{ printf "%s:%s" .name .root.Values.image.tag | quote }}
imagePullPolicy: {{ .root.Values.image.pullPolicy }}
{{- end }}

{{/* Origin browsers use for a public host, e.g. http://compliance.localhost:8081 */}}
{{- define "compliance.publicOrigin" -}}
{{- $p := .root.Values.public -}}
{{- $scheme := ternary "https" "http" $p.secure -}}
{{- $defaultPort := ternary 443 80 $p.secure -}}
{{- if eq (int $p.port) $defaultPort -}}
{{ $scheme }}://{{ .host }}
{{- else -}}
{{ $scheme }}://{{ .host }}:{{ $p.port }}
{{- end -}}
{{- end }}

{{/* The one Zitadel issuer URL, used by browsers, web and api-go alike. */}}
{{- define "compliance.zitadelIssuer" -}}
{{ include "compliance.publicOrigin" (dict "root" . "host" .Values.public.zitadelHost) }}
{{- end }}

{{- define "compliance.webURL" -}}
{{ include "compliance.publicOrigin" (dict "root" . "host" .Values.public.webHost) }}
{{- end }}

{{/*
hostAliases so server-side callers resolve the public Zitadel hostname to the
zitadel-public Service (fixed ClusterIP, listening on the public port). This is
the Kubernetes counterpart of the compose network alias: one issuer URL
everywhere, no Host-header overrides.
*/}}
{{- define "compliance.zitadelHostAliases" -}}
{{- if .Values.zitadelAlias.enabled }}
hostAliases:
  - ip: {{ .Values.zitadelAlias.clusterIP | quote }}
    hostnames:
      - {{ .Values.public.zitadelHost | quote }}
{{- end }}
{{- end }}

{{/* Postgres DSN for a role, built from the CNPG read-write Service. */}}
{{- define "compliance.postgresURL" -}}
{{- $pg := .root.Values.postgres -}}
{{- printf "postgres://%s:%s@%s-rw:5432/%s?sslmode=%s" .user (.password | urlquery) $pg.clusterName .database $pg.sslMode -}}
{{- end }}

{{/* Writable scratch dirs for a read-only root filesystem. */}}
{{- define "compliance.tmpVolume" -}}
- name: tmp
  emptyDir:
    sizeLimit: 64Mi
{{- end }}

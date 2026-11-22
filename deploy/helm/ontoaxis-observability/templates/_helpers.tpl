{{/*
Common helpers shared by every OntoAxis lifecycle chart.
These helpers are copied verbatim into each chart's templates/_helpers.tpl so
that every chart can be rendered independently (no chart library dependency).

Contract recap:
  - all images render as repository@sha256:<64 hex> (never a tag, never :latest)
  - standard labels app.kubernetes.io/{name,instance,managed-by} + ontoaxis.io/component
  - global.environment must be one of kind | kind-small | staging-like | prod-like
*/}}

{{/* Chart name */}}
{{- define "ontoaxis.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{/*
Standard labels. Call with a dict: (dict "name" $componentKey "root" $).
*/}}
{{- define "ontoaxis.labels" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
app.kubernetes.io/part-of: ontoaxis
app.kubernetes.io/version: {{ .root.Chart.AppVersion | quote }}
ontoaxis.io/component: {{ .name }}
ontoaxis.io/environment: {{ .root.Values.global.environment }}
{{- end -}}

{{/* Selector labels (stable subset). Call with (dict "name" $componentKey "root" $). */}}
{{- define "ontoaxis.selectorLabels" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
ontoaxis.io/component: {{ .name }}
{{- end -}}

{{/*
Validate global.environment. Renders nothing; fails on unknown environment so a
mis-set --values file can never silently deploy with default values.
*/}}
{{- define "ontoaxis.validateEnvironment" -}}
{{- $env := .Values.global.environment -}}
{{- $allowed := list "kind" "kind-small" "staging-like" "prod-like" -}}
{{- if not (has $env $allowed) -}}
{{- fail (printf "ontoaxis: unsupported global.environment %q (allowed: kind, kind-small, staging-like, prod-like)" $env) -}}
{{- end -}}
{{- end -}}

{{/*
Image reference helper. Call with (dict "image" $component.image).
Renders repository@digest. Fails when:
  - repository missing
  - digest missing/empty
  - digest not sha256:<64 lowercase hex>
  - repository contains :latest
  - repository carries a tag (tag-without-digest form)
*/}}
{{- define "ontoaxis.image" -}}
{{- $img := .image -}}
{{- if not $img -}}{{- fail "ontoaxis.image: image block required" -}}{{- end -}}
{{- $repo := required "ontoaxis.image: image.repository is required" $img.repository -}}
{{- $digest := required (printf "ontoaxis.image: image.digest is required for %s" $repo) $img.digest -}}
{{- if not (regexMatch "^sha256:[0-9a-f]{64}$" $digest) -}}
{{- fail (printf "ontoaxis.image: image.digest for %s must match sha256:[0-9a-f]{64}, got %q" $repo $digest) -}}
{{- end -}}
{{- if contains ":latest" $repo -}}
{{- fail (printf "ontoaxis.image: repository %q must not contain :latest" $repo) -}}
{{- end -}}
{{- if regexMatch ":[A-Za-z0-9_][A-Za-z0-9_.-]*$" $repo -}}
{{- fail (printf "ontoaxis.image: repository %q must not include a tag; use repository + digest only" $repo) -}}
{{- end -}}
{{- printf "%s@%s" $repo $digest -}}
{{- end -}}

{{/*
Render startup/readiness/liveness probes. Call with
(dict "probes" $component.probes "root" $).
mode=http -> httpGet on path/readyPath; mode=tcp -> tcpSocket.
*/}}
{{- define "ontoaxis.probes" -}}
{{- $p := .probes -}}
{{- $port := $p.port | default "http" -}}
{{- if eq $p.mode "tcp" }}
livenessProbe:
  tcpSocket:
    port: {{ $port }}
  initialDelaySeconds: {{ $p.initialDelaySeconds | default 10 }}
  periodSeconds: {{ $p.periodSeconds | default 10 }}
  timeoutSeconds: {{ $p.timeoutSeconds | default 3 }}
  failureThreshold: {{ $p.failureThreshold | default 3 }}
readinessProbe:
  tcpSocket:
    port: {{ $port }}
  initialDelaySeconds: {{ $p.initialDelaySeconds | default 5 }}
  periodSeconds: {{ $p.periodSeconds | default 10 }}
  timeoutSeconds: {{ $p.timeoutSeconds | default 3 }}
  failureThreshold: {{ $p.failureThreshold | default 3 }}
{{- else }}
startupProbe:
  httpGet:
    path: {{ $p.path | default "/healthz" }}
    port: {{ $port }}
  failureThreshold: {{ $p.startupFailureThreshold | default 30 }}
  periodSeconds: {{ $p.startupPeriodSeconds | default 5 }}
livenessProbe:
  httpGet:
    path: {{ $p.path | default "/healthz" }}
    port: {{ $port }}
  initialDelaySeconds: {{ $p.initialDelaySeconds | default 10 }}
  periodSeconds: {{ $p.periodSeconds | default 10 }}
  timeoutSeconds: {{ $p.timeoutSeconds | default 3 }}
  failureThreshold: {{ $p.failureThreshold | default 3 }}
readinessProbe:
  httpGet:
    path: {{ $p.readyPath | default "/readyz" }}
    port: {{ $port }}
  initialDelaySeconds: {{ $p.initialDelaySeconds | default 5 }}
  periodSeconds: {{ $p.periodSeconds | default 10 }}
  timeoutSeconds: {{ $p.timeoutSeconds | default 3 }}
  failureThreshold: {{ $p.failureThreshold | default 3 }}
{{- end }}
{{- end -}}

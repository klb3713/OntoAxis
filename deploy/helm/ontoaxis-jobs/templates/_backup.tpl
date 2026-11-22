{{/*
Shared pod/job spec for the backup CronJob and the optional manual Job.
Emits the body of a batch/v1 Job `spec:` so both callers stay in sync.
*/}}
{{- define "ontoaxis.backup.jobSpec" -}}
backoffLimit: {{ .Values.backup.backoffLimit | default 0 }}
activeDeadlineSeconds: {{ .Values.backup.activeDeadlineSeconds | default 3600 }}
ttlSecondsAfterFinished: 86400
template:
  metadata:
    labels:
      {{- include "ontoaxis.labels" (dict "name" "backup" "root" .) | nindent 6 }}
  spec:
    restartPolicy: Never
    serviceAccountName: {{ .Values.backup.serviceAccount.name | default "ontoaxis-backup" }}
    securityContext:
      fsGroup: 999
    containers:
      - name: backup
        image: {{ include "ontoaxis.image" (dict "image" .Values.backup.image) }}
        imagePullPolicy: {{ .Values.global.imagePullPolicy | default "IfNotPresent" }}
        command: ["/bin/sh", "-c", "/opt/backup/backup.sh"]
        env:
          - name: PGHOST
            value: {{ required "ontoaxis-jobs: backup.pgHost is required" .Values.backup.pgHost | quote }}
          - name: PGPORT
            value: {{ .Values.backup.pgPort | default "5432" | quote }}
          - name: PGDATABASE
            valueFrom:
              secretKeyRef:
                name: {{ required "ontoaxis-jobs: backup.secretName is required" .Values.backup.secretName }}
                key: {{ required "ontoaxis-jobs: backup.secretKeys.database is required" .Values.backup.secretKeys.database }}
          - name: PGUSER
            valueFrom:
              secretKeyRef:
                name: {{ required "ontoaxis-jobs: backup.secretName is required" .Values.backup.secretName }}
                key: {{ required "ontoaxis-jobs: backup.secretKeys.user is required" .Values.backup.secretKeys.user }}
          - name: PGPASSWORD
            valueFrom:
              secretKeyRef:
                name: {{ required "ontoaxis-jobs: backup.secretName is required" .Values.backup.secretName }}
                key: {{ required "ontoaxis-jobs: backup.secretKeys.password is required" .Values.backup.secretKeys.password }}
          - name: BACKUP_DIR
            value: "/backups"
          - name: RETENTION
            value: {{ .Values.backup.retention | default 7 | quote }}
          - name: IMAGE_DIGEST
            value: {{ include "ontoaxis.image" (dict "image" .Values.backup.image) | quote }}
          {{- if .Values.backup.schemaVersion }}
          - name: SCHEMA_VERSION
            value: {{ .Values.backup.schemaVersion | quote }}
          {{- end }}
        resources:
          {{- toYaml .Values.backup.resources | nindent 10 }}
        volumeMounts:
          - name: backups
            mountPath: /backups
          - name: backup-script
            mountPath: /opt/backup
            readOnly: true
    volumes:
      - name: backups
        persistentVolumeClaim:
          claimName: {{ required "ontoaxis-jobs: backup.pvc.name is required" .Values.backup.pvc.name }}
      - name: backup-script
        configMap:
          name: {{ .Values.backup.name }}-script
          defaultMode: 0755
{{- end -}}

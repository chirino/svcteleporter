package install

var exporterOpenshiftTemplate = `
apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: Secret
  metadata:
    name: svcteleporter-exporter
  data:
    config.yaml: >-
      {{.ExporterConfigBase64}}
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: svcteleporter-exporter
    labels:
      app: svcteleporter-exporter
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: svcteleporter-exporter
    template:
      metadata:
        labels:
          app: svcteleporter-exporter
      spec:
        volumes:
          - name: config-volume
            secret:
              secretName: svcteleporter-exporter
        containers:
          - name: svcteleporter-import
            image: quay.io/hchirino/svcteleporter:latest
            imagePullPolicy: Always
            command: [ "/usr/local/bin/svcteleporter", "exporter", "/config/config.yaml" ]
            volumeMounts:
              - name: config-volume
                mountPath: /config
`

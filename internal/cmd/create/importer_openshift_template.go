package create

var importerOpenshiftTemplate = `
apiVersion: v1
kind: List
items:
- apiVersion: route.openshift.io/v1
  kind: Route
  metadata:
    name: svcteleporter-importer-ws
  spec:
    port:
      targetPort: 8443
    tls:
      termination: passthrough
    to:
      kind: Service
      name: svcteleporter-importer-ws
- apiVersion: v1
  kind: Service
  metadata:
    name: svcteleporter-importer-ws
  spec:
    selector:
      app: svcteleporter-importer
    ports:
      - port: 8443
        protocol: TCP
        targetPort: 8443
{{range $val := .ImporterConfig.Services}}
- apiVersion: v1
  kind: Service
  metadata:
    name: "{{$val.ProxyService}}"
  spec:
    selector:
      app: svcteleporter-importer
    ports:
      - protocol: TCP
        port: {{$val.ProxyPort}}
        targetPort: {{$val.ProxyPort}}
{{end}}

- apiVersion: v1
  kind: Secret
  metadata:
    name: svcteleporter-importer
  data:
    config.yaml: >-
       {{.ImporterConfigBase64}}
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: svcteleporter-importer
    labels:
      app: svcteleporter-importer
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: svcteleporter-importer
    template:
      metadata:
        labels:
          app: svcteleporter-importer
      spec:
        volumes:
          - name: config-volume
            secret:
              secretName: svcteleporter-importer
        containers:
          - name: svcteleporter-import
            image: quay.io/hchirino/svcteleporter:latest
            imagePullPolicy: Always
            command: [ "/usr/local/bin/svcteleporter", "importer", "/config/config.yaml" ]
            volumeMounts:
              - name: config-volume
                mountPath: /config
            ports:
              - containerPort: 8443
{{range $val := .ImporterConfig.Services}}
              - containerPort: {{$val.ProxyPort}}
{{end}}
`

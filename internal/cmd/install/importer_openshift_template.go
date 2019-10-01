package install

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
      targetPort: 1443
    tls:
      insecureEdgeTerminationPolicy: None
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
      - port: 1443
        protocol: TCP
        targetPort: 1443
{{range $i,$val := .ImporterConfig.Services}}
- apiVersion: v1
  kind: Service
  metadata:
    name: "{{$val.KubeService}}"
  spec:
    selector:
      app: svcteleporter-importer
    ports:
      - protocol: TCP
        port: {{$val.KubePort}}
        targetPort: {{add 2000 $i}}
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
              - containerPort: 1443
{{range $i, $val := .ImporterConfig.Services}}
              - containerPort: {{add 2000 $i}}
{{end}}
`

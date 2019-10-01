package install

import (
    "bytes"
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/base64"
    "encoding/pem"
    "flag"
    "fmt"
    "github.com/chirino/svcteleporter/internal/cmd"
    "github.com/chirino/svcteleporter/internal/pkg/utils"
    "github.com/spf13/cobra"
    "io/ioutil"
    "log"
    "math/big"
    "sigs.k8s.io/yaml"
    "strings"
    "text/template"
    "time"

    "k8s.io/client-go/dynamic"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func New() *cobra.Command {
    o := Options{
        Proxies: []cmd.ProxySpec{},
    }
    command := &cobra.Command{Use: `install [[kube-service[:port],]target-host:target:port]+`}

    // Lets rexport the flags installed by the controller runtime, and make them a little less kube specific
    f := *flag.CommandLine.Lookup("kubeconfig")
    f.Name = "config"
    f.Usage = "path to the config file to connect to the cluster"
    command.PersistentFlags().AddGoFlag(&f)

    f = *flag.CommandLine.Lookup("master")
    f.Usage = "the address of the cluster API server."
    command.PersistentFlags().AddGoFlag(&f)

    // cmd.PersistentFlags().StringVar(&options.KubeConfig, "config", , "path to the config file to connect to the cluster")
    namespace, _ := GetClientNamespace(o.KubeConfig)
    command.PersistentFlags().StringVarP(&o.Namespace, "namespace", "n", namespace, "namespace to run against")

    command.Flags().StringVar(&o.ImporterHostPort, "importer-host-port", "", "The public hostname:port the importer will run at")
    command.Flags().StringVar(&o.Prefix, "config-prefix", "", "config file prefix")
    command.Flags().DurationVar(&o.Duration, "duration", 10*365*24*time.Hour, "duration that mutual TLS certificates will be valid for")
    command.Flags().IntVar(&o.KeySize, "key-size", 4096, "size of RSA key to generate.")
    command.Flags().StringArrayVar(&o.Kinds, "output", []string{"openshift", "standalone"}, "the types of configuration outputs to generate. on of: openshif or standalone.")
    command.RunE = func(c *cobra.Command, args []string) (err error) {
        if len(args) < 1 {
            return fmt.Errorf("invalid usage. expecting:[proxy-port:target-host:target:port]+")
        }
        for _, arg := range args {
            proxy, err := cmd.ParseProxySpec(arg)
            if err != nil {
                return err
            }
            o.Proxies = append(o.Proxies, proxy)
        }
        utils.ExitOnError(ConfigFiles(o))
        return nil
    }
    return command
}

type Options struct {
    KubeConfig string
    Namespace  string

    KeySize  int
    Duration time.Duration
    Prefix   string
    Kinds    []string

    ImporterHostPort string
    Proxies          []cmd.ProxySpec
}

type RenderScope struct {
    ImporterConfig       *cmd.ImporterConfig
    ExporterConfig       *cmd.ExporterConfig
    ImporterConfigBase64 string
    ExporterConfigBase64 string
}

func ConfigFiles(o Options) (err error) {
    outputKinds := map[string]bool{}
    for _, value := range o.Kinds {
        outputKinds[strings.ToLower(value)] = true
    }
    ic := cmd.ImporterConfig{
        Listen:   "0.0.0.0:1443",
        Services: o.Proxies,
    }
    ec := cmd.ExporterConfig{
        ImporterHostPort: o.ImporterHostPort,
        Proxies:          o.Proxies,
    }

    scope := RenderScope{
        ImporterConfig: &ic,
        ExporterConfig: &ec,
    }

    ic.Cert, ic.Key, err = createCertificate(o.KeySize, o.Duration, "importer")
    if err != nil {
        return err
    }
    ec.Cert, ec.Key, err = createCertificate(o.KeySize, o.Duration, "exporter")
    if err != nil {
        return err
    }
    ic.CAs = []string{ec.Cert}
    ec.CAs = []string{ic.Cert}

    icm, err := yaml.Marshal(ic)
    if err != nil {
        return err
    }
    scope.ImporterConfigBase64 = base64.StdEncoding.EncodeToString(icm)
    ecm, err := yaml.Marshal(ec)
    if err != nil {
        return err
    }
    scope.ExporterConfigBase64 = base64.StdEncoding.EncodeToString(ecm)

    if outputKinds["standalone"] {
        err = writeFile(o.Prefix+"standalone-importer.yaml", icm)
        if err != nil {
            return err
        }

        err = writeFile(o.Prefix+"standalone-exporter.yaml", ecm)
        if err != nil {
            return err
        }

    }

    if outputKinds["openshift"] {
        resources, err := Render(importerOpenshiftTemplate, scope)
        if err != nil {
            return err
        }
        err = writeFile(o.Prefix+"openshift-importer.yaml", []byte(resources))
        if err != nil {
            return err
        }

        resources, err = Render(exporterOpenshiftTemplate, scope)
        if err != nil {
            return err
        }
        err = writeFile(o.Prefix+"openshift-exporter.yaml", []byte(resources))
        if err != nil {
            return err
        }
    }

    fmt.Println("")
    fmt.Println("These files contain secrets.  Please be careful sharing them.")

    return nil
}

func writeFile(name string, data []byte) error {
    err := ioutil.WriteFile(name, data, 0600)
    if err != nil {
        return err
    }
    fmt.Println("wrote: ", name)
    return nil
}

func Render(goTemplate string, scope interface{}) (string, error) {
    t, err := template.New("Render").Funcs(map[string]interface{}{
        "add": func(a int, b int) int {
            return a + b
        },
    }).Parse(goTemplate)
    if err != nil {
        return "", err
    }
    buffer := bytes.NewBuffer(nil)
    err = t.Execute(buffer, scope)
    if err != nil {
        return "", err
    }
    s := buffer.String()
    return s, nil
}

func createCertificate(keySize int, duration time.Duration, commonName string) (publicCert string, privateKey string, err error) {
    priv, err := rsa.GenerateKey(rand.Reader, keySize)
    if err != nil {
        return "", "", fmt.Errorf("failed to generate private key: %s", err)
    }
    notBefore := time.Now()
    notAfter := notBefore.Add(duration)
    serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
    if err != nil {
        log.Fatalf("failed to generate serial number: %s", err)
    }
    template := x509.Certificate{
        SerialNumber: serialNumber,
        Subject: pkix.Name{
            Organization: []string{"localhost"},
            CommonName:   commonName,
        },
        NotBefore:             notBefore,
        NotAfter:              notAfter,
        IsCA:                  true,
        KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
        ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
        BasicConstraintsValid: true,
        DNSNames:              []string{commonName},
    }
    derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
    if err != nil {
        log.Fatalf("Failed to create certificate: %s", err)
    }
    certOut := bytes.NewBuffer(nil)
    pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
    cert := certOut.String()
    keyOut := bytes.NewBuffer(nil)
    pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
    key := keyOut.String()
    return cert, key, nil
}

func (o *Options) GetClientConfig() *rest.Config {
    c, err := config.GetConfig()
    utils.ExitOnError(err)
    return c
}

func (o *Options) GetClient() (c client.Client, err error) {
    return client.New(o.GetClientConfig(), client.Options{})
}

func (o *Options) NewDynamicClient() (c dynamic.Interface, err error) {
    return dynamic.NewForConfig(o.GetClientConfig())
}

func (o *Options) NewApiClient() (*kubernetes.Clientset, error) {
    return kubernetes.NewForConfig(o.GetClientConfig())
}

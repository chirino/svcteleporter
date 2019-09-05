package create

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
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
)

func New() *cobra.Command {
	o := Options{
		Proxies: []cmd.ProxySpec{},
	}
	command := &cobra.Command{Use: `create importer-host-name [proxy-port:target-host:target:port]+`}
	command.Flags().StringVar(&o.Prefix, "config-prefix", "", "config file prefix")
	command.Flags().DurationVar(&o.Duration, "duration", 10*365*24*time.Hour, "duration that mutual TLS certificates will be valid for")
	command.Flags().IntVar(&o.KeySize, "key-size", 4096, "size of RSA key to generate.")
	command.Flags().StringArrayVar(&o.Kinds, "output", []string{"openshift", "standalone"}, "the types of configuration outputs to generate. on of: openshif or standalone.")
	command.RunE = func(c *cobra.Command, args []string) (err error) {
		if len(args) < 2 {
			return fmt.Errorf("invalid usage. expecting: importer-host-name [proxy-port:target-host:target:port]+")
		}
		o.ImporterUrl = "wss://" + args[0]
		o.WebSocketPort = 8443
		for _, arg := range args[1:] {
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
	KeySize  int
	Duration time.Duration
	Prefix   string
	Kinds    []string

	ImporterUrl   string
	Proxies       []cmd.ProxySpec
	WebSocketPort uint32
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
		Services:      o.Proxies,
		WebSocketPort: o.WebSocketPort,
	}
	ec := cmd.ExporterConfig{
		ImporterUrl: o.ImporterUrl,
		Proxies:     o.Proxies,
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
	t, err := template.New("Render").Parse(goTemplate)
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

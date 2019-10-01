package exporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/chirino/svcteleporter/internal/cmd"
	"github.com/chirino/svcteleporter/internal/pkg/utils"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sigs.k8s.io/yaml"
	"time"
)

var ImporterHostPort=""

func New() *cobra.Command {

	command := &cobra.Command{
		Use: `exporter`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting a config file argument")
			}
			log.Println("svcteleporter version:", cmd.Version)
			config, err := LoadConfigFile(args[0])
			utils.ExitOnError(err)
			err = Serve(context.Background(), config)
			utils.ExitOnError(err)
			return nil
		},
	}
	command.Flags().StringVar(&ImporterHostPort, "importer-host-port", "", "The public hostname:port the importer runs at")
	return command
}

func LoadConfigFile(configFile string) (*cmd.ExporterConfig, error) {
	bytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	config := &cmd.ExporterConfig{}
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(bytes, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func Serve(ctx context.Context, config *cmd.ExporterConfig) error {
	publicKeyPem := []byte(config.Cert)
	privateKeyPem := []byte(config.Key)
	cert, err := tls.X509KeyPair(publicKeyPem, privateKeyPem)
	if err != nil {
		return err
	}
	caPool := x509.NewCertPool()
	for _, ca := range config.CAs {
		caPool.AppendCertsFromPEM([]byte(ca))
	}

	if ImporterHostPort != "" {
		config.ImporterHostPort = ImporterHostPort
	}
	host, _, err := net.SplitHostPort(config.ImporterHostPort)
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		ServerName:   host,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,

		// Lets skip hostname verification, but lets check it's a trusted cert.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {

			certs := make([]*x509.Certificate, len(rawCerts))
			for i, asn1Data := range rawCerts {
				cert, err := x509.ParseCertificate(asn1Data)
				if err != nil {
					return err
				}
				certs[i] = cert
			}

			opts := x509.VerifyOptions{
				Roots:         caPool,
				CurrentTime:   time.Now(),
				DNSName:       certs[0].DNSNames[0],
				Intermediates: x509.NewCertPool(),
			}

			verifiedChains, err := certs[0].Verify(opts)
			if err != nil {
				return err
			}

			return nil
		},
	}

	tlsConfig.BuildNameToCertificate()

	log.Println("exporter:tls dialing:", config.ImporterHostPort)
	tlsConn, err := tls.Dial("tcp", config.ImporterHostPort, tlsConfig)
	if err != nil {
		return err
	}

	sshConfig := &ssh.ClientConfig{
		User: "testuser",
		Auth: []ssh.AuthMethod{},
	}
	if sshConfig.HostKeyCallback == nil {
		sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	c, chans, reqs, err := ssh.NewClientConn(tlsConn, config.ImporterHostPort, sshConfig)
	if err != nil {
		return err
	}
	sshConnection := ssh.NewClient(c, chans, reqs)

	results := make(chan error)
	for i, service := range config.Proxies {

		targetAddressPort := fmt.Sprintf("%s:%d", service.UpstreamHost, service.UpstreamPort)

		// Listen on remote server port
		log.Println("exporter:opening listener for service ", targetAddressPort)
		remoteHostPortListen, err := sshConnection.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", 2000+i))
		if err != nil {
			return fmt.Errorf("export error: %s", err)
		}

		go func() {
			defer remoteHostPortListen.Close()
			for {
				sshTunnel, err := remoteHostPortListen.Accept()
				if err != nil {
					results <- err
					return
				}
				go onNewConnectionForward(sshTunnel, targetAddressPort)
			}
			results <- nil
			log.Println("exporter:closing listener for service ", targetAddressPort)
			return
		}()
	}

	for range config.Proxies {
		err := <-results
		if err != nil {
			return err
		}
	}
	return nil
}

func onNewConnectionForward(sshTunnel net.Conn, targetAddress string) {

	log.Println("exporter:tunnel dialing upstream:", targetAddress)
	targetConn, err := net.Dial("tcp", targetAddress)
	if err != nil {
		sshTunnel.Close()
		log.Println("exporter:tunnel dial error:", err)
		return
	}

	// Start remote -> local data transfer
	go func() {
		defer sshTunnel.Close()
		defer targetConn.Close()

		_, err := io.Copy(sshTunnel, targetConn)
		if err != nil {
			log.Println("exporter:tunnel <- upstream: error: ", err)
		}
		log.Println("exporter:tunnel <- upstream: closed")
	}()

	// Start local -> remote data transfer
	go func() {
		defer sshTunnel.Close()
		defer targetConn.Close()
		_, err := io.Copy(targetConn, sshTunnel)
		if err != nil {
			log.Println("exporter:tunnel -> upstream: error: ", err)
		}
		log.Println("exporter:tunnel -> upstream: closed")
	}()
}

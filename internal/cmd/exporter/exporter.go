package exporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/chirino/svcteleporter/internal/cmd"
	"github.com/chirino/svcteleporter/internal/pkg/utils"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sigs.k8s.io/yaml"
	"time"
)

func New() *cobra.Command {
	return &cobra.Command{
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
	head := map[string][]string{}

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

	tlsConfig := &tls.Config{
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

	log.Println("exporter:websocket dialing:", config.ImporterUrl)
	dialer := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}
	websocketConnection, resp, err := dialer.DialContext(ctx, config.ImporterUrl, head)
	if err != nil && resp != nil {
		err = fmt.Errorf("%s: http response code: %d %s\n", err, resp.StatusCode, resp.Status)
	}
	if err != nil {
		return err
	}
	defer websocketConnection.Close()
	wsNetConn := utils.WebSocketToNetConn(context.Background(), websocketConnection, "exporter:wss ")

	sshConfig := &ssh.ClientConfig{
		User: "testuser",
		Auth: []ssh.AuthMethod{},
	}
	if sshConfig.HostKeyCallback == nil {
		sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	c, chans, reqs, err := ssh.NewClientConn(wsNetConn, config.ImporterUrl, sshConfig)
	if err != nil {
		return err
	}
	sshConnection := ssh.NewClient(c, chans, reqs)

	results := make(chan error)
	for _, service := range config.Proxies {

		targetAddressPort := fmt.Sprintf("%s:%d", service.UpstreamHost, service.UpstreamPort)

		// Listen on remote server port
		log.Println("exporter:opening listener for service ", targetAddressPort)
		serviceListener, err := sshConnection.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", service.ProxyPort))
		if err != nil {
			return fmt.Errorf("export error: %s", err)
		}

		go func() {
			defer serviceListener.Close()
			for {
				sshTunnel, err := serviceListener.Accept()
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

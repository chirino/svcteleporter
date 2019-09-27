package importer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/chirino/ssh"
	"github.com/chirino/svcteleporter/internal/cmd"
	"github.com/chirino/svcteleporter/internal/pkg/utils"
	"github.com/spf13/cobra"
	ssh2 "golang.org/x/crypto/ssh"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sigs.k8s.io/yaml"
	"time"
)

func New() *cobra.Command {
	var httpPort uint32 = 1001
	var servicePort uint32 = 1002
	command := &cobra.Command{
		Use: `importer`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting a config file argument")
			}

			log.Println("svcteleporter version:", cmd.Version)

			config, err := LoadConfigFile(args[0])
			utils.ExitOnError(err)

			importer, err := NewFromConfig(context.Background(), config)
			utils.ExitOnError(err)

			listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", config.WebSocketPort))
			utils.ExitOnError(err)

			err = importer.Serve(listener)
			utils.ExitOnError(err)
			return nil
		},
	}
	command.Flags().Uint32VarP(&httpPort, "tunnel-port", "", httpPort, "The port the tunnel is established on.")
	command.Flags().Uint32VarP(&servicePort, "service-port", "", servicePort, "The service port which is tunneled to the remote")
	return command
}

func LoadConfigFile(configFile string) (*cmd.ImporterConfig, error) {
	bytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	config := &cmd.ImporterConfig{}
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(bytes, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

type importer struct {
	context        context.Context
	TLSConfig *tls.Config
	sshServer      ssh.Server
}

func NewFromConfig(context context.Context, config *cmd.ImporterConfig) (*importer, error) {
	result := &importer{
		context:        context,
		sshServer: newSshServer(config.Services...),
	}

	publicKeyPem := []byte(config.Cert)
	privateKeyPem := []byte(config.Key)
	cert, err := tls.X509KeyPair(publicKeyPem, privateKeyPem)
	if err != nil {
		return nil, err
	}
	caPool := x509.NewCertPool()
	for _, ca := range config.CAs {
		caPool.AppendCertsFromPEM([]byte(ca))
	}
	result.TLSConfig = &tls.Config{
		ClientAuth:               tls.RequireAndVerifyClientCert,
		ClientCAs:                caPool,
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
		Certificates:             []tls.Certificate{cert},
	}
	result.TLSConfig.BuildNameToCertificate()
	return result, nil
}

func (this *importer) Serve(listener net.Listener) error {
	defer listener.Close()
	l := tls.NewListener(listener, this.TLSConfig)
	log.Println(l.Addr())
	for {
		conn, e := l.Accept()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				tempDelay := 5 * time.Millisecond
				log.Println("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return e
		}
		go this.sshServer.HandleConn(conn)
	}
}

func newSshServer(servicePorts ...cmd.ProxySpec) ssh.Server {
	allowedPorts := map[uint32]bool{}
	for _, value := range servicePorts {
		allowedPorts[value.ProxyPort] = true
	}
	forwardHandler := &ForwardedTCPHandler{}
	server := ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			return true
		}),
		Handler: ssh.Handler(func(s ssh.Session) {
			io.WriteString(s, "Remote forwarding available...\n")
			select {}
		}),
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			if host == "0.0.0.0" && allowedPorts[port] {
				return true
			} else {
				return false
			}
		}),
		ServerConfigCallback: func(ctx ssh.Context) *ssh2.ServerConfig {
			config := &ssh2.ServerConfig{}
			// config.Ciphers
			return config
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
	}
	return server
}
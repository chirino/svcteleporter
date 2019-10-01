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
    gossh "golang.org/x/crypto/ssh"
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

            listener, err := net.Listen("tcp", config.Listen)
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
    context   context.Context
    TLSConfig *tls.Config
    sshServer ssh.Server
}

func NewFromConfig(context context.Context, config *cmd.ImporterConfig) (*importer, error) {
    result := &importer{
        context:   context,
        sshServer: newSshServer(config),
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
    log.Println("listening on:", l.Addr())
    for {
        conn, err := l.Accept()
        if err != nil {
            log.Println("accept error:", err)
            if ne, ok := err.(net.Error); ok && ne.Temporary() {
                tempDelay := 5 * time.Millisecond
                log.Printf("tls: Accept error: %v; retrying in %v\n", err, tempDelay)
                time.Sleep(5 * time.Millisecond)
                continue
            }
            return err
        }
        log.Println("accepted connection from:", conn.RemoteAddr())
        go this.sshServer.HandleConn(conn)
    }
}

func newSshServer(config *cmd.ImporterConfig) ssh.Server {
    forwardHandler := &ForwardedTCPHandler{config: config}
    server := ssh.Server{
        LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
            return true
        }),
        Handler: ssh.Handler(func(s ssh.Session) {
            io.WriteString(s, "Remote forwarding available...\n")
            select {}
        }),
        ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
            if host == "0.0.0.0" && 2000 <= port && port <= 2000+uint32(len(config.Services)) {
                return true
            } else {
                return false
            }
        }),
        ServerConfigCallback: func(ctx ssh.Context) *gossh.ServerConfig {
            config := &gossh.ServerConfig{}

            signer, err := gossh.ParsePrivateKey([]byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQC8A6FGHDiWCSREAXCq6yBfNVr0xCVG2CzvktFNRpue+RXrGs/2
a6ySEJQb3IYquw7HlJgu6fg3WIWhOmHCjfpG0PrL4CRwbqQ2LaPPXhJErWYejcD8
Di00cF3677+G10KMZk9RXbmHtuBFZT98wxg8j+ZsBMqGM1+7yrWUvynswQIDAQAB
AoGAJMCk5vqfSRzyXOTXLGIYCuR4Kj6pdsbNSeuuRGfYBeR1F2c/XdFAg7D/8s5R
38p/Ih52/Ty5S8BfJtwtvgVY9ecf/JlU/rl/QzhG8/8KC0NG7KsyXklbQ7gJT8UT
Ojmw5QpMk+rKv17ipDVkQQmPaj+gJXYNAHqImke5mm/K/h0CQQDciPmviQ+DOhOq
2ZBqUfH8oXHgFmp7/6pXw80DpMIxgV3CwkxxIVx6a8lVH9bT/AFySJ6vXq4zTuV9
6QmZcZzDAkEA2j/UXJPIs1fQ8z/6sONOkU/BjtoePFIWJlRxdN35cZjXnBraX5UR
fFHkePv4YwqmXNqrBOvSu+w2WdSDci+IKwJAcsPRc/jWmsrJW1q3Ha0hSf/WG/Bu
X7MPuXaKpP/DkzGoUmb8ks7yqj6XWnYkPNLjCc8izU5vRwIiyWBRf4mxMwJBAILa
NDvRS0rjwt6lJGv7zPZoqDc65VfrK2aNyHx2PgFyzwrEOtuF57bu7pnvEIxpLTeM
z26i6XVMeYXAWZMTloMCQBbpGgEERQpeUknLBqUHhg/wXF6+lFA+vEGnkY+Dwab2
KCXFGd+SQ5GdUcEMe9isUH6DYj/6/yCDoFrXXmpQb+M=
-----END RSA PRIVATE KEY-----
`))
            if err != nil {
                panic(fmt.Sprintf("Unable to parse host key: %v", err))
            }
            config.AddHostKey(signer)
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

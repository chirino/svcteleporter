package cmd_test

import (
	"context"
	"github.com/chirino/svcteleporter/internal/cmd"
	"github.com/chirino/svcteleporter/internal/cmd/create"
	"github.com/chirino/svcteleporter/internal/cmd/exporter"
	"github.com/chirino/svcteleporter/internal/cmd/importer"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"log"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func FatalOnError(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestEndToEnd(t *testing.T) {
	assert := assert.New(t)

	log.Println("Opening ports...")
	// Open the port of a mock service
	mockSvcListener, err := net.Listen("tcp", "127.0.0.1:0")
	FatalOnError(t, err)
	targetPort, err := strconv.Atoi(getPort(mockSvcListener))
	FatalOnError(t, err)
	targetHost := "127.0.0.1"

	// Open the port the ssh over ws service.
	sslListener, err := net.Listen("tcp", "127.0.0.1:0")
	FatalOnError(t, err)
	sslPort, err := strconv.Atoi(getPort(sslListener))
	FatalOnError(t, err)

	// Now that we know the ports that we will be using.. lets create the config
	log.Println("Generating certs and config...")
	err = create.ConfigFiles(create.Options{
		WebSocketPort: uint32(sslPort),
		Kinds:         []string{"standalone"},
		Duration:      24 * time.Hour,
		KeySize:       4096,
		Prefix:        ".test-",
		ImporterUrl:   "127.0.0.1:" + getPort(sslListener),
		Proxies: []cmd.ProxySpec{
			cmd.ProxySpec{
				ProxyPort:    2000,
				UpstreamHost: targetHost,
				UpstreamPort: uint32(targetPort),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run the importer...
	go func() {
		importerConfig, err := importer.LoadConfigFile(".test-standalone-importer.yaml")
		FatalOnError(t, err)
		importer, err := importer.NewFromConfig(context.Background(), importerConfig)
		FatalOnError(t, err)
		err = importer.Serve(sslListener)
		FatalOnError(t, err)
	}()

	// Run the exporter...
	go func() {
		exporterConfig, err := exporter.LoadConfigFile(".test-standalone-exporter.yaml")
		FatalOnError(t, err)
		exporter.Serve(context.Background(), exporterConfig)
	}()

	time.Sleep(1 * time.Second)
	// Do a request against the importer proxy port..
	go func() {
		// connect to the exported service...
		// it might take a few tries before the exporter and importer are online..
		var conn net.Conn
		for i := 1; ; i++ {
			c, err := net.Dial("tcp", "localhost:2000")
			if err == nil {
				conn = c
				break
			}
			log.Printf("Connect attempt %d error: %s\n", i, err)
			time.Sleep(1 * time.Second)
		}
		conn.Write([]byte(`hello!`))
		conn.Close()
	}()

	// the mock service should get the connection and the data sent to the importer.
	log.Println("mock service waiting for a connection on", getPort(mockSvcListener))
	conn, err := mockSvcListener.Accept()
	if err != nil {
		t.Fatal(err)
	}

	log.Println("mock service got a connection on", getPort(mockSvcListener))
	data, err := ioutil.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}

	text := string(data)
	assert.Equal(`hello!`, text)
}

func getPort(listener net.Listener) string {
	addr := strings.Split(listener.Addr().String(), ":")
	return addr[len(addr)-1]
}

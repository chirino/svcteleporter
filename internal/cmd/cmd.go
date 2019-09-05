package cmd

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

var Version string = "latest"

type ImporterConfig struct {
	Cert          string
	Key           string
	CAs           []string
	WebSocketPort uint32
	Services      []ProxySpec
}

type ExporterConfig struct {
	Cert        string
	Key         string
	CAs         []string
	ImporterUrl string
	Proxies     []ProxySpec
}

type ProxySpec struct {
	ProxyService string
	ProxyPort    uint32
	UpstreamHost string
	UpstreamPort uint32
}

func (p *ProxySpec) String() string {
	return fmt.Sprintf("%s:%d,%s:%d", p.ProxyService, p.ProxyPort, p.UpstreamHost, p.UpstreamPort)
}

func ParseProxySpec(service string) (spec ProxySpec, err error) {
	splits := strings.Split(service, ",")
	if len(splits) != 2 {
		err = fmt.Errorf("Invalid format, expecting: name:port,name:port")
		return
	}

	port := ""
	spec.ProxyService, port, err = net.SplitHostPort(strings.TrimSpace(splits[0]))
	if err != nil {
		return
	}
	i := 0
	i, err = strconv.Atoi(port)
	if err != nil {
		return
	}
	spec.ProxyPort = uint32(i)

	spec.UpstreamHost, port, err = net.SplitHostPort(strings.TrimSpace(splits[1]))
	if err != nil {
		return
	}
	i, err = strconv.Atoi(port)
	if err != nil {
		return
	}
	spec.UpstreamPort = uint32(i)
	return
}

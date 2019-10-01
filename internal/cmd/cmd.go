package cmd

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var Version string = "latest"

type ImporterConfig struct {
	Cert     string
	Key      string
	CAs      []string
	Listen   string
	Services []ProxySpec
}

type ExporterConfig struct {
	Cert             string
	Key              string
	CAs              []string
	ImporterHostPort string
	Proxies          []ProxySpec
}

type ProxySpec struct {
	KubeService  string
	KubePort     uint32
	UpstreamHost string
	UpstreamPort uint32
}

func (p *ProxySpec) String() string {
	return fmt.Sprintf("%s:%d,%s:%d", p.KubeService, p.KubePort, p.UpstreamHost, p.UpstreamPort)
}

func ParseProxySpec(service string) (spec ProxySpec, err error) {
	splits := strings.Split(service, ",")
	if len(splits) > 2 {
		err = fmt.Errorf("Invalid format, expecting: [[kube-service[:port],]target-host:target:port]")
		return
	}

	i := 0
	host, port, err := net.SplitHostPort(strings.TrimSpace(splits[0]))
	if err != nil {
		return
	}
	i, err = strconv.Atoi(port)
	if err != nil {
		return
	}
	if len(splits) == 1 {
		spec.KubeService = sanitizeKubeService(host)
		spec.KubePort = uint32(i)

		spec.UpstreamHost = host
		spec.UpstreamPort = uint32(i)
	} else {
		spec.UpstreamHost = host
		spec.UpstreamPort = uint32(i)

		host, port, err = net.SplitHostPort(strings.TrimSpace(splits[1]))
		if err != nil {
			return
		}
		i, err = strconv.Atoi(port)
		if err != nil {
			return
		}
		spec.KubeService = host
		spec.KubePort = uint32(i)
	}
	return
}

func sanitizeKubeService(name string) string {
	return regexp.MustCompile("[^a-zA-Z0-9]+").ReplaceAllString(name, "-")
}

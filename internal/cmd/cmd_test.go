package cmd

import (
	"github.com/magiconair/properties/assert"
	"testing"
)

func TestParseProxySpec(t *testing.T) {
	spec, err := ParseProxySpec("21:test")
	if err == nil {
		t.FailNow()
	}
	spec, err = ParseProxySpec("wow:21,test:23")
	if err != nil {
		t.FailNow()
	}
	assert.Equal(t, spec, ProxySpec{
		ProxyService: "wow",
		ProxyPort:    21,
		UpstreamHost: "test",
		UpstreamPort: 23,
	})

}

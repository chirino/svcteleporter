package main

import (
	"flag"
	"github.com/chirino/svcteleporter/internal/cmd/svcteleporter"
	"github.com/chirino/svcteleporter/internal/pkg/utils"
	"github.com/spf13/pflag"
	"math/rand"
	"os"
	"time"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	err := svcteleporter.New().Execute()
	switch err {
	case flag.ErrHelp:
		fallthrough
	case pflag.ErrHelp:
		svcteleporter.New().Help()
		os.Exit(0)
	default:
		utils.ExitOnError(err)
	}
}

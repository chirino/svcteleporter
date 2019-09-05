// +build mage

package main

import (
    "github.com/chirino/hawtgo/sh"
    "github.com/magefile/mage/mg"
    "os"
)

/////////////////////////////////////////////////////////////////////////
// A little setup to make defining the build targets easier
/////////////////////////////////////////////////////////////////////////
var (
    // d is for dependencies
    d   = mg.Deps
    cli = sh.New().
        CommandLog(os.Stdout).
        CommandLogPrefix("running > ").
        Dir("..")
)

/////////////////////////////////////////////////////////////////////////
// Build Targets:
/////////////////////////////////////////////////////////////////////////
var Default = All

func All() {
    d(Build, Test)
}

type Platform struct {
    GOOS   string
    GOARCH string
}

func Format() {
    cli.Line(`go fmt ./... `).MustZeroExit()
}

func Build() {
    gitHash, _, _ := cli.Line(`git log -1 --pretty=format:%h `).Output()
    cli.
        Env(map[string]string{
            "GOOS":   "linux",
            "GOARCH": "amd64",
        }).
        Line(`go build -ldflags "-X github.com/chirino/svcteleporter/internal/cmd.Version=latest#`+ gitHash +`"  -o image/dist/svcteleporter main.go`).
        MustZeroExit()
    cli.Line(`go build -ldflags "-X github.com/chirino/svcteleporter/internal/cmd.Version=latest#`+ gitHash +`"  -o svcteleporter main.go`).
        MustZeroExit()
}

func Test() {
    d(Format)
    cli.Line(`go test ./... `).MustZeroExit()
}

func Image() {
	d(Build)
	cli.
		Line(`docker build image -t quay.io/hchirino/svcteleporter`).
		MustZeroExit()
}

func Push() {
    d(Image)
    cli.
        Line(`docker push quay.io/hchirino/svcteleporter`).
        MustZeroExit()
}


func Changelog() {
    cli.Line(`go run github.com/git-chglog/git-chglog/cmd/git-chglog`).MustZeroExit()
}

type Catalog mg.Namespace

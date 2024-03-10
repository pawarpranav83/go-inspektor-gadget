package main

import (
	IG "github.com/pawarpranav83/go-inspektor-gadget/ig"
)

func main() {
	ig, err := IG.New(IG.Path("ig"), IG.Image("ghcr.io/inspektor-gadget/gadget/trace_tcpconnect:latest"))
	if err != nil {
		panic(err)
	}

	if err := ig.Remove(); err != nil {
		panic(err)
	}
}

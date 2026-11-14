package main

import (
	"os"

	"github.com/soub4i/forge/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}

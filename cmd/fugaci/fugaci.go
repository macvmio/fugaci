package main

import (
	"github.com/macvmio/fugaci/cmd/fugaci/cmd"
)

func main() {
	cmd.Execute(cmd.InitializeCommands())
}

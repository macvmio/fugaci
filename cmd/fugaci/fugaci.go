package main

import (
	"github.com/tomekjarosik/fugaci/cmd/fugaci/cmd"
)

func main() {
	cmd.Execute(cmd.InitializeCommands())
}

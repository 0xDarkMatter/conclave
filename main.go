package main

import "github.com/0xDarkMatter/conclave-cli/cmd"

var version = "dev"

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}

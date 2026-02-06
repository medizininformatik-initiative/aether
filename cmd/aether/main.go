/*
Copyright © 2025 Aether Contributors
*/
package main

import "github.com/medizininformatik-initiative/aether/cmd"

// version is set at build time via ldflags:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}

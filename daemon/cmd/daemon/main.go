package main

import "log"

var Version = "v2.2.0"

func main() {
	if err := runDaemon(parseDaemonOptions()); err != nil {
		log.Fatal(err)
	}
}

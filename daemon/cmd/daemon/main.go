package main

import "log"

var Version = "v2.2.11"

func main() {
	if err := runDaemon(parseDaemonOptions()); err != nil {
		log.Fatal(err)
	}
}

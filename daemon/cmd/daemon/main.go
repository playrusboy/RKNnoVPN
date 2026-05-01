package main

import "log"

var Version = "v2.2.8"

func main() {
	if err := runDaemon(parseDaemonOptions()); err != nil {
		log.Fatal(err)
	}
}

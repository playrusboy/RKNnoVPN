package main

import "log"

var Version = "v2.1.6"

func main() {
	if err := runDaemon(parseDaemonOptions()); err != nil {
		log.Fatal(err)
	}
}

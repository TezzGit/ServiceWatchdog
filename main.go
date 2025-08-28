// https://dev.to/cosmic_predator/writing-a-windows-service-in-go-1d1m

package main

import (
	"fmt"
	"log"
	"os"
)

const DEBUG_MODE = true

func main() {
	f, err := os.OpenFile("debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	resolver := &DefaultServiceManagerResolver{
		Connector: &DefaultSCMConnector{},
	}

	watcher, err := loadConfig("config.json", resolver)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	runWatcherService("serviceWatcher", DEBUG_MODE, watcher)
}

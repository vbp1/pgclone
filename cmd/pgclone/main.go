package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		fmt.Println("Running in agent mode")
		// TODO: agent.Run()
	} else {
		fmt.Println("Running in client mode")
		// TODO: client.Run()
	}
}



package main

import (
	"fmt"
	"log"

	"github.com/vbp1/pgclone/internal/cli"
)

func main() {
	fmt.Println("pgclone (Go) – work in progress")
	if err := cli.Execute(); err != nil {
		log.Fatal(err)
	}
}

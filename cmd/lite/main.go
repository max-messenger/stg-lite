package main

import (
	"context"
	"flag"
	"log"

	"lite/internal/app"
)

func main() {
	var (
		cfgName string
	)

	flag.StringVar(&cfgName, "c", "config.yaml", "config")
	flag.Parse()

	a, err := app.New(context.Background(), "lite", cfgName)
	if err != nil {
		log.Fatal(err)
	}

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

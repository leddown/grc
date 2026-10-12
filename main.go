package main

import (
	"flag"
	"log"

	"grc/internal/app"
)

func main() {
	options := app.BindFlags()
	flag.Parse()

	if err := app.Run(*options); err != nil {
		log.Fatal(err)
	}
}

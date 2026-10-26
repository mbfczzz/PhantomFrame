package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"phantomframe/internal/api"
)

func main() {
	path := flag.String("file", "", "UTF-8 provider inventory CSV (never printed)")
	apply := flag.Bool("apply", false, "import routes and encrypted keys; new models stay disabled until priced")
	flag.Parse()
	if *path == "" {
		log.Fatal("--file is required")
	}
	file, err := os.Open(*path)
	if err != nil {
		log.Fatal("cannot open inventory file")
	}
	defer file.Close()
	var report api.Row
	if !*apply {
		_, report, err = api.ReadProviderInventory(file)
	} else {
		cfg, e := api.LoadConfig()
		if e != nil {
			log.Fatal(e)
		}
		app, e := api.Open(cfg)
		if e != nil {
			log.Fatal(e)
		}
		defer app.Close()
		if e = app.Migrate(); e != nil {
			log.Fatal(e)
		}
		report, err = app.ImportProviderInventory(file, true)
	}
	if err != nil {
		log.Fatal(err)
	}
	output, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(output))
}

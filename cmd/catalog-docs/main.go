package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"phantomframe/internal/api"
	"time"
)

func main() {
	apply := flag.Bool("apply", false, "apply the reviewed defaults; stop the API first")
	snapshot := flag.String("baseline", "data/maintenance/catalog-publication-2026-10-25T11-35-36-143Z.json", "initial publication snapshot")
	sources := flag.String("sources", "test-results/model-docs-20261025", "saved supplier model responses")
	flag.Parse()
	c, e := api.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	if *apply {
		conn, e := net.DialTimeout("tcp", net.JoinHostPort(c.Host, c.Port), time.Second)
		if e == nil {
			conn.Close()
			log.Fatal("stop the API before applying model defaults")
		}
	}
	a, e := api.Open(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	result, e := a.BackfillVendorDocumentation(*snapshot, *sources, *apply)
	if e != nil {
		log.Fatal(e)
	}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		log.Fatal(e)
	}
	directory := filepath.Join(c.DataDir, "maintenance")
	if e = os.MkdirAll(directory, 0700); e != nil {
		log.Fatal(e)
	}
	suffix := "plan"
	if *apply {
		suffix = "applied"
	}
	if e = os.WriteFile(filepath.Join(directory, "model-documentation-"+suffix+".json"), b, 0600); e != nil {
		log.Fatal(e)
	}
	fmt.Println(api.DocumentationSummary(result))
}

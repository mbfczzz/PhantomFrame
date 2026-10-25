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
	apply := flag.Bool("apply", false, "apply the reviewed 20 percent markup plan")
	verify := flag.Bool("verify", false, "verify installed prices without supplier calls")
	path := flag.String("plan", "data/maintenance/supplier-price-plan.json", "price plan")
	flag.Parse()
	c, e := api.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	if *apply {
		conn, e := net.DialTimeout("tcp", net.JoinHostPort(c.Host, c.Port), time.Second)
		if e == nil {
			conn.Close()
			log.Fatal("stop the API before applying prices")
		}
	}
	a, e := api.Open(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	if *apply {
		if e = a.Migrate(); e != nil {
			log.Fatal(e)
		}
	}
	var result api.Row
	if *verify {
		result, e = a.VerifySupplierPrices(*path)
	} else {
		result, e = a.ImportSupplierPrices(*path, *apply)
	}
	if e != nil {
		log.Fatal(e)
	}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		log.Fatal(e)
	}
	if *apply {
		if e = os.WriteFile(filepath.Join(c.DataDir, "maintenance/supplier-pricing-applied.json"), b, 0600); e != nil {
			log.Fatal(e)
		}
	}
	fmt.Println(string(b))
}

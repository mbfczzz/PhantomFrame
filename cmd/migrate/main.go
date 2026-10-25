package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"phantomframe/internal/api"
)

func main() {
	file := flag.String("file", "data/migration/sqlite-export.json", "SQLite JSON export path")
	flag.Parse()
	c, e := api.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	a, e := api.Open(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	if e = a.Migrate(); e != nil {
		log.Fatal(e)
	}
	counts, e := a.ImportSQLite(*file)
	if e != nil {
		log.Fatal(e)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"imported": counts, "database": c.MySQLDatabase, "backend": "go"})
}

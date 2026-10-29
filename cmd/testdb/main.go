// Test-only database lifecycle utility. Production names are always refused.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"phantomframe/internal/api"
	"regexp"
)

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "create" && os.Args[1] != "drop" && os.Args[1] != "seed-demo") || !regexp.MustCompile(`^phantomframe_test_[a-z0-9_]+$`).MatchString(os.Args[2]) {
		log.Fatal("usage: testdb create|drop|seed-demo phantomframe_test_<unique-name>")
	}
	c, e := api.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	c.MySQLDatabase = ""
	if os.Args[1] == "seed-demo" {
		c.MySQLDatabase = os.Args[2]
		c.SupplierInstances, c.ProviderPresets, c.Demo = false, false, true
	}
	a, e := api.Open(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	if os.Args[1] == "seed-demo" {
		if a.Store.One("SELECT id FROM users WHERE role='admin' AND status='active' LIMIT 1") == nil {
			log.Fatal("Start the isolated test application before seeding fixtures")
		}
		a.Seed()
		fmt.Println("Explicit isolated demo fixtures seeded")
		return
	}
	query := "CREATE DATABASE `" + os.Args[2] + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
	if os.Args[1] == "drop" {
		query = "DROP DATABASE IF EXISTS `" + os.Args[2] + "`"
	}
	if _, e = a.DB.Exec(query); e != nil {
		log.Fatal(e)
	}
	if os.Args[1] == "drop" {
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := a.Redis.Scan(ctx, cursor, os.Args[2]+":*", 100).Result()
			if err != nil {
				log.Fatal(err)
			}
			if len(keys) > 0 {
				if err = a.Redis.Del(ctx, keys...).Err(); err != nil {
					log.Fatal(err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	fmt.Println("Test database", os.Args[1], "completed")
}

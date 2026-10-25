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
	if len(os.Args) != 3 || (os.Args[1] != "create" && os.Args[1] != "drop") || !regexp.MustCompile(`^phantomframe_test_[a-z0-9_]+$`).MatchString(os.Args[2]) {
		log.Fatal("usage: testdb create|drop phantomframe_test_<unique-name>")
	}
	c, e := api.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	c.MySQLDatabase = ""
	a, e := api.Open(c)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
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

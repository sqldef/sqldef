package main

import (
	"github.com/go-sql-driver/mysql"
)

func buildMysqlConfig() *mysql.Config {
	config := mysql.NewConfig()
	return config
}

func main() {
	println("Usage: schemasql [sql file]")
	buildMysqlConfig()
}

package main

import (
	"database/sql"
	"log"

	"github.com/go-sql-driver/mysql"
)

func buildMysqlDSN() string {
	config := mysql.NewConfig()
	config.User = "root"
	config.Passwd = ""
	config.Net = "tcp"
	config.Addr = "127.0.0.1:3306"
	config.DBName = "test"
	return config.FormatDSN()
}

func parseTable() {
	dsn := buildMysqlDSN()
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	var table string
	var ddl string
	err = conn.QueryRow("show create table user;").Scan(&table, &ddl)
	if err != nil {
		log.Fatal(err)
	}

	var ddl2 string
	err = conn.QueryRow("show create table user2;").Scan(&table, &ddl2)
	if err != nil {
		log.Fatal(err)
	}
}

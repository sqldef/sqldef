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

func main() {
	dsn := buildMysqlDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	transaction, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	sql := `
		CREATE TABLE user (
		  id BIGINT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY,
		  name VARCHAR(191) UNIQUE,
		  salt VARCHAR(20),
		  password VARCHAR(40),
		  display_name TEXT,
		  avatar_icon TEXT,
		  created_at DATETIME NOT NULL
		) Engine=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	if _, err := transaction.Exec(sql); err != nil {
		transaction.Rollback()
		log.Fatal(err)
	}
	transaction.Commit()
	println("success!")
}

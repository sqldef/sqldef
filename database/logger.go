package database

import "fmt"

type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
	Println(v ...any)
}

type StdoutLogger struct{}

func (s StdoutLogger) Print(v ...any) {
	fmt.Print(v...)
}

func (s StdoutLogger) Printf(format string, v ...any) {
	fmt.Printf(format, v...)
}

func (s StdoutLogger) Println(v ...any) {
	fmt.Println(v...)
}

type NullLogger struct{}

func (n NullLogger) Print(v ...any)                 {}
func (n NullLogger) Printf(format string, v ...any) {}
func (n NullLogger) Println(v ...any)               {}

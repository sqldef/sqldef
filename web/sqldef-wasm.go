// This is a light wasm wraper around just the DDL conversion stuff
// You don't need to include this in your website.
package main
 
import (
	"log"
	"strings"
  "syscall/js"
  "github.com/k0kubun/sqldef/schema"
)

func diff(this js.Value, args []js.Value) interface {} {
	mode := args[0].String()
	desiredDDLs := args[1].String()
	currentDDLs := args[2].String()
	callback := args[3]
	generatorMode := schema.GeneratorModeMysql
	if (mode == "postgres"){
		generatorMode = schema.GeneratorModePostgres
	}
	ddls, err := schema.GenerateIdempotentDDLs(generatorMode, desiredDDLs, currentDDLs)
	out := strings.Join(ddls, ";\n")
	callback.Invoke(js.Null(), out)

	// TODO: Need to figure out how to pass error in callback
	if err != nil {
    log.Fatal(err)
	}
	_ = err
	return true
}
 
func main() {
	c := make(chan bool)
	// I wish this wasn't global!
  js.Global().Set("_SQLDEF", js.FuncOf(diff))
  <-c
}

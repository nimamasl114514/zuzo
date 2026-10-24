package main

import (
	"fmt"
	"os"

	"zuzo/py/lexer"
)

func main() {
	data, _ := os.ReadFile(os.Args[1])
	l := lexer.New(string(data))
	for _, t := range l.Tokens() {
		fmt.Printf("%-10s %-30q line=%d col=%d extra=%s\n", t.Type, t.Literal, t.Line, t.Column, t.Extra)
	}
	for _, e := range l.Errors() {
		fmt.Println("ERR:", e)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	fmt.Println("graph-benchmark: cross-database graph benchmarking tool")
	fmt.Println("Configure credentials via .env (see .env.example). Do not hardcode secrets.")
	if len(os.Args) > 1 {
		fmt.Printf("args: %v\n", os.Args[1:])
	}
}

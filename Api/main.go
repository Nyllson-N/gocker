package main

import (
	"fmt"
	"github.com/Nyllson-N/godocker"
)

func main() {
	// Criar um cliente Docker usando detecção automática
	client := godocker.New()

	fmt.Println("Cliente Docker criado com sucesso!")
}

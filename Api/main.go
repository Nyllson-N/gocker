package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/Nyllson-N/gocker"
)

func main() {
	client := gocker.NewLAN("192.168.1.3")

	http.HandleFunc("/containers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		// Obter todos os containers (incluindo parados)
		result, err := client.Get("/containers/json?all=true")
		if err != nil {
			http.Error(w, fmt.Sprintf("Erro ao obter containers: %v", err), http.StatusInternalServerError)
			return
		}

		// Formatar o JSON com indentação de 2 espaços
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, []byte(result), "", "  "); err != nil {
			http.Error(w, fmt.Sprintf("Erro ao formatar JSON: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(prettyJSON.Bytes())
	})

	fmt.Println("Servidor iniciado na porta 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

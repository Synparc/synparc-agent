package main

import (
	"encoding/json"
	"log"
	"os"
)

type Config struct {
	ServerUrl  string `json:"serverUrl"`
	ProxyUrl   string `json:"proxyUrl,omitempty"`
	AgentToken string `json:"agentToken,omitempty"`
}

var AppConfig Config

func LoadConfig() {
	// Valeurs par défaut
	token := os.Getenv("AGENT_TOKEN")
	AppConfig = Config{
		ServerUrl:  "http://localhost:3001/api/agent",
		ProxyUrl:   "",
		AgentToken: token,
	}

	file, err := os.Open("config.json")
	if err != nil {
		log.Println("ℹ️ Aucun fichier config.json trouvé, utilisation des paramètres par défaut.")
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&AppConfig); err != nil {
		log.Printf("⚠️ Erreur lors de la lecture de config.json : %v. Utilisation des paramètres par défaut.\n", err)
	} else {
		log.Println("✅ Configuration chargée depuis config.json")
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

const SERVER_URL = "http://localhost:3000/api/agent"

type CheckinPayload struct {
	AdGuid      string `json:"adGuid"`
	Hostname    string `json:"hostname"`
	Fqdn        string `json:"fqdn"`
	MachineType string `json:"machineType"`
	OsName      string `json:"osName"`
	OsVersion   string `json:"osVersion"`
}

type CheckinResponse struct {
	Status    string `json:"status"`
	MachineId string `json:"machineId"`
}

type MetricRecord struct {
	Time          string  `json:"time"`
	CpuPercent    float64 `json:"cpuPercent"`
	RamPercent    float64 `json:"ramPercent"`
	UptimeSeconds uint64  `json:"uptimeSeconds"`
}

type MetricsPayload struct {
	MachineId string         `json:"machineId"`
	Metrics   []MetricRecord `json:"metrics"`
}

func main() {
	log.Println("🚀 Démarrage de l'agent Synparc...")

	// 1. Récupération des infos systèmes
	hostStat, err := host.Info()
	if err != nil {
		log.Fatalf("Erreur lors de la récupération des infos hôte : %v", err)
	}

	hostname := hostStat.Hostname
	osName := hostStat.Platform
	osVersion := hostStat.PlatformVersion

	// Pour la v1, on génère un faux AD GUID constant basé sur le hostname
	// Dans la version finale, on lira le vrai objectGUID de la machine dans le registre Windows.
	adGuid := "00000000-0000-0000-0000-000000000001" // PC Local par défaut

	payload := CheckinPayload{
		AdGuid:      adGuid,
		Hostname:    hostname,
		Fqdn:        hostname + ".synparc.local", // Mock
		MachineType: "workstation",
		OsName:      osName,
		OsVersion:   osVersion,
	}

	// 2. Check-in
	log.Println("📡 Envoi du Check-in au serveur...")
	jsonData, _ := json.Marshal(payload)
	
	resp, err := http.Post(SERVER_URL+"/checkin", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("❌ Impossible de joindre le serveur (%s) : %v", SERVER_URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("❌ Le serveur a rejeté le check-in (Status: %d)", resp.StatusCode)
	}

	var checkinResp CheckinResponse
	json.NewDecoder(resp.Body).Decode(&checkinResp)

	machineId := checkinResp.MachineId
	log.Printf("✅ Check-in réussi ! Machine ID : %s\n", machineId)

	// 3. Boucle d'envoi des métriques
	log.Println("📊 Début de la collecte des métriques (toutes les 10 secondes)...")
	
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Collecte
		v, _ := mem.VirtualMemory()
		c, _ := cpu.Percent(0, false) // Global CPU
		h, _ := host.Info()

		cpuUsage := 0.0
		if len(c) > 0 {
			cpuUsage = c[0]
		}

		record := MetricRecord{
			Time:          time.Now().UTC().Format(time.RFC3339),
			CpuPercent:    cpuUsage,
			RamPercent:    v.UsedPercent,
			UptimeSeconds: h.Uptime,
		}

		metricsPayload := MetricsPayload{
			MachineId: machineId,
			Metrics:   []MetricRecord{record},
		}

		metricsJson, _ := json.Marshal(metricsPayload)

		// Envoi (en goroutine pour ne pas bloquer le ticker)
		go func(data []byte) {
			r, err := http.Post(SERVER_URL+"/metrics", "application/json", bytes.NewBuffer(data))
			if err != nil {
				log.Printf("⚠️ Erreur d'envoi des métriques : %v\n", err)
				return
			}
			r.Body.Close()
			if r.StatusCode == 200 {
				log.Printf("📈 Métriques envoyées (CPU: %.1f%%, RAM: %.1f%%)\n", record.CpuPercent, record.RamPercent)
			} else {
				log.Printf("⚠️ Serveur a refusé les métriques (Status: %d)\n", r.StatusCode)
			}
		}(metricsJson)
	}
}

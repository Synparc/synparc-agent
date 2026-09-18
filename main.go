package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)


type CheckinPayload struct {
	AdGuid      string `json:"adGuid"`
	Hostname    string `json:"hostname"`
	Fqdn        string `json:"fqdn"`
	MachineType string `json:"machineType"`
	OsName      string `json:"osName"`
	OsVersion   string `json:"osVersion"`
	LocalIp     string `json:"localIp"`
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

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// ---- RESILIENCE ENGINE ----

func getResilientHostname(hostStat *host.InfoStat) string {
	// M1: gopsutil
	if hostStat != nil && hostStat.Hostname != "" {
		return hostStat.Hostname
	}
	// M2: os.Hostname natif Go
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	// M3: exec shell
	if out, err := exec.Command("hostname").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "UNKNOWN-PC"
}

func getResilientOsName(hostStat *host.InfoStat) string {
	if hostStat != nil && hostStat.Platform != "" {
		return hostStat.Platform
	}
	if osEnv := os.Getenv("OS"); osEnv != "" {
		return osEnv
	}
	return "Unknown Windows"
}

func getResilientOsVersion(hostStat *host.InfoStat) string {
	if hostStat != nil && hostStat.PlatformVersion != "" {
		return hostStat.PlatformVersion
	}
	if out, err := exec.Command("cmd", "/c", "ver").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "Unknown Version"
}

func getResilientCpuPercent() float64 {
	// M1
	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		return percents[0]
	}
	// M2: wmic
	if out, err := exec.Command("wmic", "cpu", "get", "loadpercentage").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			if val, err := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64); err == nil {
				return val
			}
		}
	}
	// M3
	return 0.0
}

func getResilientRamPercent() float64 {
	// M1
	if vm, err := mem.VirtualMemory(); err == nil {
		return vm.UsedPercent
	}
	// M2: wmic
	if out, err := exec.Command("wmic", "OS", "get", "FreePhysicalMemory,TotalVisibleMemorySize").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			parts := strings.Fields(lines[1])
			if len(parts) >= 2 {
				free, _ := strconv.ParseFloat(parts[0], 64)
				total, _ := strconv.ParseFloat(parts[1], 64)
				if total > 0 {
					return ((total - free) / total) * 100.0
				}
			}
		}
	}
	// M3
	return 0.0
}

type SmbShareItem struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	LocalPath    string `json:"localPath"`
	Description  string `json:"description"`
	ResourceType string `json:"resourceType"`
}

type SharesPayload struct {
	MachineId string         `json:"machineId"`
	Hostname  string         `json:"hostname"`
	Shares    []SmbShareItem `json:"shares"`
}

func getResilientSmbShares(hostname string) []SmbShareItem {
	shares := []SmbShareItem{}
	seenPaths := make(map[string]bool)

	// 1. Mapped Network Drives (Get-SmbMapping) - e.g. I:\, U:\, A:\
	cmdMapping := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-SmbMapping | Select-Object LocalPath, RemotePath, Status | ConvertTo-Json -Compress")
	if out, err := cmdMapping.Output(); err == nil && len(out) > 0 {
		var mapItems []struct {
			LocalPath  string `json:"LocalPath"`
			RemotePath string `json:"RemotePath"`
		}
		if errJson := json.Unmarshal(out, &mapItems); errJson == nil {
			for _, item := range mapItems {
				if item.RemotePath != "" {
					p := item.RemotePath
					if !seenPaths[strings.ToLower(p)] {
						seenPaths[strings.ToLower(p)] = true
						name := item.LocalPath
						if name == "" {
							name = p
						}
						shares = append(shares, SmbShareItem{
							Name:         fmt.Sprintf("Disque Mappé %s (%s)", name, p),
							Path:         p,
							LocalPath:    item.LocalPath,
							Description:  "Lecteur réseau connecté",
							ResourceType: "mapped_drive",
						})
					}
				}
			}
		} else {
			var singleMap struct {
				LocalPath  string `json:"LocalPath"`
				RemotePath string `json:"RemotePath"`
			}
			if errSingle := json.Unmarshal(out, &singleMap); errSingle == nil && singleMap.RemotePath != "" {
				p := singleMap.RemotePath
				if !seenPaths[strings.ToLower(p)] {
					seenPaths[strings.ToLower(p)] = true
					name := singleMap.LocalPath
					if name == "" {
						name = p
					}
					shares = append(shares, SmbShareItem{
						Name:         fmt.Sprintf("Disque Mappé %s (%s)", name, p),
						Path:         p,
						LocalPath:    singleMap.LocalPath,
						Description:  "Lecteur réseau connecté",
						ResourceType: "mapped_drive",
					})
				}
			}
		}
	}

	// 2. Local SMB Shares (Get-SmbShare)
	cmdPS := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-SmbShare | Where-Object { $_.Name -ne 'IPC$' -and $_.Name -ne 'ADMIN$' } | Select-Object Name, Path, Description | ConvertTo-Json -Compress")
	if out, err := cmdPS.Output(); err == nil && len(out) > 0 {
		var psItems []struct {
			Name        string `json:"Name"`
			Path        string `json:"Path"`
			Description string `json:"Description"`
		}
		if errJson := json.Unmarshal(out, &psItems); errJson == nil {
			for _, item := range psItems {
				if item.Name != "" {
					p := "\\\\" + hostname + "\\" + item.Name
					if !seenPaths[strings.ToLower(p)] {
						seenPaths[strings.ToLower(p)] = true
						shares = append(shares, SmbShareItem{
							Name:         item.Name,
							Path:         p,
							LocalPath:    item.Path,
							Description:  item.Description,
							ResourceType: "smb_share",
						})
					}
				}
			}
		} else {
			var singleItem struct {
				Name        string `json:"Name"`
				Path        string `json:"Path"`
				Description string `json:"Description"`
			}
			if errSingle := json.Unmarshal(out, &singleItem); errSingle == nil && singleItem.Name != "" {
				p := "\\\\" + hostname + "\\" + singleItem.Name
				if !seenPaths[strings.ToLower(p)] {
					seenPaths[strings.ToLower(p)] = true
					shares = append(shares, SmbShareItem{
						Name:         singleItem.Name,
						Path:         p,
						LocalPath:    singleItem.Path,
						Description:  singleItem.Description,
						ResourceType: "smb_share",
					})
				}
			}
		}
	}

	// 3. Fallback: Logical Disks (Win32_LogicalDisk)
	cmdDisk := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_LogicalDisk | Select-Object DeviceID, VolumeName, DriveType | ConvertTo-Json -Compress")
	if out, err := cmdDisk.Output(); err == nil && len(out) > 0 {
		var diskItems []struct {
			DeviceID   string `json:"DeviceID"`
			VolumeName string `json:"VolumeName"`
			DriveType  int    `json:"DriveType"`
		}
		if errJson := json.Unmarshal(out, &diskItems); errJson == nil {
			for _, d := range diskItems {
				p := "\\\\" + hostname + "\\" + strings.ReplaceAll(d.DeviceID, ":", "$")
				if !seenPaths[strings.ToLower(p)] {
					seenPaths[strings.ToLower(p)] = true
					label := d.VolumeName
					if label == "" {
						label = "Disque"
					}
					shares = append(shares, SmbShareItem{
						Name:         fmt.Sprintf("Disque %s (%s)", d.DeviceID, label),
						Path:         p,
						LocalPath:    d.DeviceID + "\\",
						Description:  fmt.Sprintf("Lecteur %s", d.DeviceID),
						ResourceType: "local_disk",
					})
				}
			}
		}
	}

	return shares
}


// FIX-08: UUID Fallback persistant (remplace le UUID hardcodé)
func getOrCreateLocalMachineUUID() string {
	const machineIdFile = "machine-id.txt"
	if data, err := os.ReadFile(machineIdFile); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) >= 32 {
			log.Printf("📶 Identifiant machine local chargé : %s", id)
			return id
		}
	}
	// Générer un nouvel UUID aléatoire
	b := make([]byte, 16)
	rand.Read(b)
	newId := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	os.WriteFile(machineIdFile, []byte(newId), 0644)
	log.Printf("🆔 Nouvel identifiant machine généré et persisté : %s", newId)
	return newId
}

func getRealSystemUuid() string {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID")
	if out, err := cmd.Output(); err == nil {
		uuidStr := strings.TrimSpace(string(out))
		if len(uuidStr) > 20 {
			return strings.ToLower(uuidStr)
		}
	}
	// FIX-08: Fallback persistant (fini le UUID hardcodé partagé par toutes les machines)
	return getOrCreateLocalMachineUUID()
}

func getRealActiveUserSession() string {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystem).UserName")
	if out, err := cmd.Output(); err == nil {
		u := strings.TrimSpace(string(out))
		if u != "" {
			return u
		}
	}
	return ""
}

// FIX-18: Persistance du machineId (survit aux redémarrages de l'agent)
type MachineState struct {
	MachineId   string `json:"machineId"`
	LastCheckin string `json:"lastCheckin"`
}

func saveMachineState(machineId string) {
	state := MachineState{MachineId: machineId, LastCheckin: time.Now().Format(time.RFC3339)}
	data, _ := json.Marshal(state)
	os.WriteFile("synparc-machine-state.json", data, 0600)
}

func loadMachineState() string {
	data, err := os.ReadFile("synparc-machine-state.json")
	if err != nil {
		return ""
	}
	var state MachineState
	if json.Unmarshal(data, &state) == nil {
		return state.MachineId
	}
	return ""
}

// FIX-05: Helper HTTP qui ajoute automatiquement le header X-Agent-Token
func doPost(client *http.Client, urlStr string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if AppConfig.AgentToken != "" {
		req.Header.Set("X-Agent-Token", AppConfig.AgentToken)
	}
	return client.Do(req)
}

// FIX-09: Check-in avec retry exponentiel (5 tentatives max)
func checkinWithRetry(client *http.Client, urlStr string, jsonData []byte, maxRetries int) (*http.Response, error) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(1<<uint(attempt)) * time.Second
			log.Printf("⏳ Tentative %d/%d dans %v...", attempt+1, maxRetries, wait)
			time.Sleep(wait)
		}
		resp, err := doPost(client, urlStr, jsonData)
		if err == nil {
			return resp, nil
		}
		log.Printf("⚠️ Check-in tentative %d échouée : %v", attempt+1, err)
	}
	return nil, fmt.Errorf("serveur inaccessible après %d tentatives", maxRetries)
}

func main() {
	log.Println("🚀 Démarrage de l'agent Synparc...")
	LoadConfig()

	// Configuration du client HTTP avec ou sans proxy explicite
	var transport *http.Transport
	if AppConfig.ProxyUrl != "" {
		proxyURL, err := url.Parse(AppConfig.ProxyUrl)
		if err != nil {
			log.Fatalf("❌ URL de proxy invalide : %v", err)
		}
		transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		log.Printf("🌐 Utilisation du proxy configuré : %s\n", AppConfig.ProxyUrl)
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	// 1. Récupération des infos systèmes réelles et résilientes
	hostStat, _ := host.Info()

	hostname := getResilientHostname(hostStat)
	osName := getResilientOsName(hostStat)
	osVersion := getResilientOsVersion(hostStat)

	// Lecture du véritable System UUID de la machine Windows
	adGuid := getRealSystemUuid()

	payload := CheckinPayload{
		AdGuid:      adGuid,
		Hostname:    hostname,
		Fqdn:        hostname + ".crn.fr",
		MachineType: "workstation",
		OsName:      osName,
		OsVersion:   osVersion,
		LocalIp:     getLocalIP(),
	}

	// 2. Check-in avec retry exponentiel (FIX-09)
	log.Println("📡 Envoi du Check-in au serveur...")
	jsonData, _ := json.Marshal(payload)

	resp, err := checkinWithRetry(httpClient, AppConfig.ServerUrl+"/checkin", jsonData, 5)
	if err != nil {
		log.Fatalf("❌ Impossible de joindre le serveur après plusieurs tentatives : %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("❌ Le serveur a rejeté le check-in (Status: %d). Vérifiez le token X-Agent-Token dans config.json.", resp.StatusCode)
	}

	var checkinResp CheckinResponse
	json.NewDecoder(resp.Body).Decode(&checkinResp)

	machineId := checkinResp.MachineId
	log.Printf("✅ Check-in réussi ! Machine ID : %s (UUID: %s)\n", machineId, adGuid)

	// FIX-18: Persister le machineId pour les redémarrages
	saveMachineState(machineId)

	// Remontée de la session utilisateur réelle
	activeUser := getRealActiveUserSession()
	if activeUser != "" {
		sessionPayload := map[string]interface{}{
			"machineId":    machineId,
			"username":     activeUser,
			"sessionStart": time.Now().UTC().Format(time.RFC3339),
			"sessionType":  "interactive",
		}
		sData, _ := json.Marshal(sessionPayload)
		// FIX-05: utiliser doPost() qui ajoute automatiquement le header X-Agent-Token
		if r, err := doPost(httpClient, AppConfig.ServerUrl+"/sessions", sData); err == nil {
			r.Body.Close()
			log.Printf("👤 Session active détectée et enregistrée pour %s\n", activeUser)
		}
	}

	// Scan & Remontée des partages SMB locaux
	go func() {
		time.Sleep(1 * time.Second)
		localShares := getResilientSmbShares(hostname)
		if len(localShares) > 0 {
			sharesPayload := SharesPayload{
				MachineId: machineId,
				Hostname:  hostname,
				Shares:    localShares,
			}
			sJson, _ := json.Marshal(sharesPayload)
			r, err := httpClient.Post(AppConfig.ServerUrl+"/shares", "application/json", bytes.NewBuffer(sJson))
			if err == nil {
				r.Body.Close()
				log.Printf("📁 %d partages SMB découverts et enregistrés au serveur central", len(localShares))
			}
		}
	}()

	// 3. Boucle d'envoi des métriques
	log.Println("📊 Début de la collecte des métriques (toutes les 10 secondes)...")
	
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Collecte
		h, _ := host.Info()
		cpuUsage := getResilientCpuPercent()
		ramUsage := getResilientRamPercent()
		
		uptime := uint64(0)
		if h != nil {
			uptime = h.Uptime
		}

		record := MetricRecord{
			Time:          time.Now().UTC().Format(time.RFC3339),
			CpuPercent:    cpuUsage,
			RamPercent:    ramUsage,
			UptimeSeconds: uptime,
		}

		metricsPayload := MetricsPayload{
			MachineId: machineId,
			Metrics:   []MetricRecord{record},
		}

		metricsJson, _ := json.Marshal(metricsPayload)

		// Envoi (en goroutine pour ne pas bloquer le ticker)
		go func(data []byte) {
			r, err := httpClient.Post(AppConfig.ServerUrl+"/metrics", "application/json", bytes.NewBuffer(data))
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

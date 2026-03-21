package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed index.html
var htmlFiles embed.FS

type Config struct {
	DefaultProvider string `toml:"default_provider"`
	APIKey          string `toml:"api_key"`
	DefaultModel    string `toml:"default_model"`
}

type SystemStatus struct {
	CPUUsage      string `json:"cpu_usage"`
	MemoryUsage   string `json:"memory_usage"`
	ServiceStatus string `json:"service_status"`
}

type ConfigRequest struct {
	ProtocolType string `json:"protocol_type"`
	APIUrl       string `json:"api_url"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
}

type ServiceControlRequest struct {
	Action string `json:"action"`
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var (
	configPath = filepath.Join(os.Getenv("HOME"), ".zeroclaw", "config.toml")
	version    = "1.2.0"
)

func main() {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/system/status", handleSystemStatus)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/service/control", handleServiceControl)
	http.HandleFunc("/api/logs", handleLogs)
	http.HandleFunc("/api/update", handleUpdate)

	log.Println("ZeroClawDash starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	content, err := htmlFiles.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Failed to load index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cpu := getCPUUsage()
	mem := getMemoryUsage()
	status := getServiceStatus()

	response := SystemStatus{
		CPUUsage:      cpu,
		MemoryUsage:   mem,
		ServiceStatus: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		loadExistingConfig(w, r)
		return
	}

	if r.Method == http.MethodPost {
		saveConfig(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func loadExistingConfig(w http.ResponseWriter, r *http.Request) {
	config, err := readConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"protocol_type": "openai",
			"api_url":       "",
			"api_key":       "",
			"default_model": "",
		})
		return
	}

	protocolType := "openai"
	if strings.HasPrefix(config.DefaultProvider, "anthropic-custom:") {
		protocolType = "anthropic"
	}

	apiUrl := ""
	if strings.Contains(config.DefaultProvider, ":") {
		parts := strings.SplitN(config.DefaultProvider, ":", 2)
		if len(parts) == 2 {
			apiUrl = parts[1]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"protocol_type": protocolType,
		"api_url":       apiUrl,
		"api_key":       config.APIKey,
		"default_model": config.DefaultModel,
	})
}

func saveConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.APIUrl == "" {
		http.Error(w, "API URL is required", http.StatusBadRequest)
		return
	}

	backupPath := configPath + ".bak"
	if err := copyFile(configPath, backupPath); err != nil && !os.IsNotExist(err) {
		http.Error(w, "Failed to backup config file", http.StatusInternalServerError)
		return
	}

	config, err := readConfig()
	if err != nil {
		config = &Config{}
	}

	providerPrefix := "custom"
	if req.ProtocolType == "anthropic" {
		providerPrefix = "anthropic-custom"
	}
	config.DefaultProvider = fmt.Sprintf("%s:%s", providerPrefix, req.APIUrl)

	if req.APIKey != "" {
		config.APIKey = req.APIKey
	}

	if req.DefaultModel != "" {
		config.DefaultModel = req.DefaultModel
	}

	if err := writeConfig(config); err != nil {
		restoreBackup(backupPath)
		http.Error(w, "Failed to write config file", http.StatusInternalServerError)
		return
	}

	if err := validateConfig(); err != nil {
		restoreBackup(backupPath)
		http.Error(w, fmt.Sprintf("Config validation failed: %v", err), http.StatusBadRequest)
		return
	}

	if err := restartService(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to restart service: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Config saved and service restarted successfully",
	})
}

func handleServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ServiceControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var cmd *exec.Cmd
	switch req.Action {
	case "start":
		cmd = exec.Command("zeroclaw", "service", "start")
	case "stop":
		cmd = exec.Command("zeroclaw", "service", "stop")
	case "restart":
		cmd = exec.Command("zeroclaw", "service", "restart")
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to execute %s: %v", req.Action, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Service %s executed successfully", req.Action),
	})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	cmd := exec.Command("journalctl", "-u", "zeroclaw", "-f", "-n", "100")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	release, err := getLatestRelease()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check for updates: %v", err), http.StatusInternalServerError)
		return
	}

	if release.TagName == version {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Already up to date",
		})
		return
	}

	assetURL := ""
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, "armv7-unknown-linux-gnueabihf") {
			assetURL = asset.URL
			break
		}
	}

	if assetURL == "" {
		http.Error(w, "No compatible binary found for your architecture", http.StatusNotFound)
		return
	}

	if err := downloadAndInstallBinary(assetURL, release.TagName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Successfully updated to version %s", release.TagName),
	})
}

func getCPUUsage() string {
	if runtime.GOOS != "linux" {
		return "N/A"
	}

	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return "0%"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) >= 8 {
				user, _ := strconv.ParseFloat(fields[1], 64)
				nice, _ := strconv.ParseFloat(fields[2], 64)
				system, _ := strconv.ParseFloat(fields[3], 64)
				idle, _ := strconv.ParseFloat(fields[4], 64)
				total := user + nice + system + idle
				usage := (user + nice + system) / total * 100
				return fmt.Sprintf("%.0f%%", usage)
			}
		}
	}

	return "0%"
}

func getMemoryUsage() string {
	if runtime.GOOS != "linux" {
		return "N/A"
	}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "N/A"
	}

	lines := strings.Split(string(data), "\n")
	memTotal := 0.0
	memAvailable := 0.0

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			value, _ := strconv.ParseFloat(fields[1], 64)
			if strings.HasPrefix(line, "MemTotal:") {
				memTotal = value
			} else if strings.HasPrefix(line, "MemAvailable:") {
				memAvailable = value
			}
		}
	}

	if memTotal > 0 {
		used := memTotal - memAvailable
		return fmt.Sprintf("%.0fMB / %.0fMB", used, memTotal)
	}

	return "N/A"
}

func getServiceStatus() string {
	cmd := exec.Command("zeroclaw", "service", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped"
	}

	if strings.Contains(string(output), "running") || strings.Contains(string(output), "active") {
		return "running"
	}

	return "stopped"
}

func readConfig() (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func writeConfig(config *Config) error {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(configPath, buf.Bytes(), 0644)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func restoreBackup(backupPath string) {
	if _, err := os.Stat(backupPath); err == nil {
		copyFile(backupPath, configPath)
	}
}

func validateConfig() error {
	for i := 0; i < 3; i++ {
		cmd := exec.Command("zeroclaw", "agent", "-m", "Hello, ZeroClaw!")
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}

		if i < 2 {
			time.Sleep(3 * time.Second)
		} else {
			return fmt.Errorf("validation failed after 3 attempts: %s", string(output))
		}
	}
	return fmt.Errorf("validation failed")
}

func restartService() error {
	cmd := exec.Command("zeroclaw", "service", "restart")
	return cmd.Run()
}

func getLatestRelease() (*GitHubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/guikeyy/zeroclaw/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func downloadAndInstallBinary(assetURL, version string) error {
	resp, err := http.Get(assetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tempFile := fmt.Sprintf("/tmp/zeroclaw-%s-armv7.tar.gz", version)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}
	defer os.Remove(tempFile)

	binaryPath := "/usr/local/bin/zeroclaw"
	backupPath := binaryPath + ".bak"

	if err := copyFile(binaryPath, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to backup binary: %v", err)
	}

	cmd := exec.Command("zeroclaw", "service", "stop")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to stop service: %v", err)
	}

	cmd = exec.Command("tar", "-xzf", tempFile, "-C", "/tmp")
	if err := cmd.Run(); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to extract archive: %v", err)
	}

	extractedBinary := "/tmp/zeroclaw"
	if _, err := os.Stat(extractedBinary); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("extracted binary not found: %v", err)
	}

	if err := copyFile(extractedBinary, binaryPath); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to copy binary: %v", err)
	}

	if err := os.Chmod(binaryPath, 0755); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to set executable permissions: %v", err)
	}

	cmd = exec.Command("zeroclaw", "service", "start")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to start service: %v", err)
	}

	cmd = exec.Command("zeroclaw", "service", "status")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: service status check failed: %v", err)
	}

	return nil
}

func restoreBinary(backupPath string) {
	if _, err := os.Stat(backupPath); err == nil {
		copyFile(backupPath, "/usr/local/bin/zeroclaw")
	}
}

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	Type   string `json:"type"`
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

type VersionCheckResponse struct {
	LocalVersion    string `json:"local_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	Message         string `json:"message"`
	Error           string `json:"error,omitempty"`
}

var (
	configPath       = filepath.Join(os.Getenv("HOME"), ".zeroclaw", "config.toml")
	version          = "1.2.0"
	operationLogChan = make(chan string, 100)
)

func main() {
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/system/status", handleSystemStatus)
	http.HandleFunc("/api/system/status/stream", handleSystemStatusStream)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/service/control", handleServiceControl)
	http.HandleFunc("/api/logs", handleLogs)
	http.HandleFunc("/api/update", handleUpdate)
	http.HandleFunc("/api/update/stream", handleUpdateStream)
	http.HandleFunc("/api/version/check", handleVersionCheck)

	log.Println("ZeroClawDash starting on :42611")
	log.Fatal(http.ListenAndServe(":42611", nil))
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

func handleSystemStatusStream(w http.ResponseWriter, r *http.Request) {
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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cpu := getCPUUsage()
			mem := getMemoryUsage()
			status := getServiceStatus()

			response := SystemStatus{
				CPUUsage:      cpu,
				MemoryUsage:   mem,
				ServiceStatus: status,
			}

			jsonData, err := json.Marshal(response)
			if err != nil {
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
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
	operationLog("[CONFIG] 开始保存配置...")
	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		operationLog("[CONFIG] 请求体解析失败: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.APIUrl == "" {
		operationLog("[CONFIG] API URL 为空")
		http.Error(w, "API URL is required", http.StatusBadRequest)
		return
	}

	backupPath := configPath + ".bak"
	operationLog("[CONFIG] 正在备份配置文件: %s -> %s", configPath, backupPath)
	if err := copyFile(configPath, backupPath); err != nil && !os.IsNotExist(err) {
		operationLog("[CONFIG] 备份失败: %v", err)
		http.Error(w, "Failed to backup config file", http.StatusInternalServerError)
		return
	}
	operationLog("[CONFIG] 备份完成")

	config, err := readConfig()
	if err != nil {
		operationLog("[CONFIG] 配置文件不存在，创建新配置")
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

	operationLog("[CONFIG] 正在写入配置文件: %s", configPath)
	if err := writeConfig(config); err != nil {
		operationLog("[CONFIG] 写入配置文件失败: %v", err)
		restoreBackup(backupPath)
		http.Error(w, "Failed to write config file", http.StatusInternalServerError)
		return
	}
	operationLog("[CONFIG] 配置文件写入完成")

	operationLog("[CONFIG] 正在验证配置...")
	if err := validateConfig(); err != nil {
		operationLog("[CONFIG] 配置验证失败: %v", err)
		restoreBackup(backupPath)
		http.Error(w, fmt.Sprintf("Config validation failed: %v", err), http.StatusBadRequest)
		return
	}
	operationLog("[CONFIG] 配置验证通过")

	operationLog("[CONFIG] 正在重启服务...")
	if err := restartService(); err != nil {
		operationLog("[CONFIG] 重启服务失败: %v", err)
		http.Error(w, fmt.Sprintf("Failed to restart service: %v", err), http.StatusInternalServerError)
		return
	}
	operationLog("[CONFIG] 服务重启成功")

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
		operationLog("[SERVICE] 请求体解析失败: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	operationLog("[SERVICE] 收到服务控制请求: action=%s, type=%s", req.Action, req.Type)
	var cmd *exec.Cmd
	serviceType := req.Type
	if serviceType == "" {
		serviceType = "daemon"
	}

	switch req.Action {
	case "start":
		if runtime.GOOS == "linux" {
			operationLog("[SERVICE] 检查 zeroclaw 服务是否已注册到 systemd")
			checkCmd := exec.Command("systemctl", "is-enabled", "zeroclaw.service")
			if err := checkCmd.Run(); err != nil {
				operationLog("[SERVICE] zeroclaw 服务未注册到 systemd，开始注册...")
				if err := registerSystemdService(); err != nil {
					operationLog("[SERVICE] 注册 systemd 服务失败: %v", err)
					http.Error(w, fmt.Sprintf("Failed to register systemd service: %v", err), http.StatusInternalServerError)
					return
				}
				operationLog("[SERVICE] systemd 服务注册成功")
			} else {
				operationLog("[SERVICE] zeroclaw 服务已注册到 systemd")
			}
		}
		cmd = exec.Command("./zeroclaw", "service", "start")
	case "stop":
		cmd = exec.Command("./zeroclaw", "service", "stop")
	case "restart":
		cmd = exec.Command("./zeroclaw", "service", "restart")
	default:
		operationLog("[SERVICE] 无效的操作: %s", req.Action)
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	operationLog("[SERVICE] 正在执行命令: %s", strings.Join(cmd.Args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		operationLog("[SERVICE] 执行失败: %v", err)
		operationLog("[SERVICE] 命令输出: %s", string(output))
		http.Error(w, fmt.Sprintf("Failed to execute %s: %v\nOutput: %s", req.Action, err, string(output)), http.StatusInternalServerError)
		return
	}

	operationLog("[SERVICE] 执行成功: %s", req.Action)
	if len(output) > 0 {
		operationLog("[SERVICE] 命令输出: %s", string(output))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Service %s executed successfully", req.Action),
	})
}

func registerSystemdService() error {
	homeDir := os.Getenv("HOME")
	zeroclawPath := filepath.Join(homeDir, "zeroclaw", "zeroclaw")
	servicePath := "/etc/systemd/system/zeroclaw.service"

	serviceContent := fmt.Sprintf(`[Unit]
Description=ZeroClaw Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
ExecStart=%s daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, filepath.Join(homeDir, "zeroclaw"), zeroclawPath)

	cmd := exec.Command("tee", servicePath)
	cmd.Stdin = strings.NewReader(serviceContent)
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	cmd = exec.Command("systemctl", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w, output: %s", err, string(output))
	}

	cmd = exec.Command("systemctl", "enable", "zeroclaw.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable service: %w, output: %s", err, string(output))
	}

	return nil
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

	if runtime.GOOS != "linux" {
		fmt.Fprintf(w, "data: [INFO] 日志流功能仅在 Linux 系统上可用 (需要 journalctl)\n\n")
		flusher.Flush()
		time.Sleep(1 * time.Second)
		return
	}

	homeDir := os.Getenv("HOME")
	logFilePath := filepath.Join(homeDir, "zeroclaw", "zeroclawdash.log")

	cmd := exec.Command("journalctl", "-u", "zeroclaw", "-f", "-n", "100")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(w, "data: [ERROR] 无法启动 journalctl 日志流: %v\n\n", err)
		fmt.Fprintf(w, "data: [INFO] 尝试读取备用日志文件: %s\n\n", logFilePath)
		flusher.Flush()
		readLogFile(w, flusher, logFilePath)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: [ERROR] 无法启动 journalctl 日志流: %v\n\n", err)
		fmt.Fprintf(w, "data: [INFO] 尝试读取备用日志文件: %s\n\n", logFilePath)
		flusher.Flush()
		readLogFile(w, flusher, logFilePath)
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

func readLogFile(w http.ResponseWriter, flusher http.Flusher, logFilePath string) {
	file, err := os.Open(logFilePath)
	if err != nil {
		fmt.Fprintf(w, "data: [ERROR] 无法打开日志文件 %s: %v\n\n", logFilePath, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [INFO] 已读取日志文件完成，等待新日志...\n\n")
	flusher.Flush()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastSize int64
	for range ticker.C {
		stat, err := file.Stat()
		if err != nil {
			continue
		}

		if stat.Size() > lastSize {
			_, err := file.Seek(lastSize, 0)
			if err != nil {
				continue
			}

			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
			lastSize = stat.Size()
		}
	}
}

func handleUpdateStream(w http.ResponseWriter, r *http.Request) {
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

	fmt.Fprintf(w, "data: [STREAM] SSE连接已建立\n\n")
	flusher.Flush()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case logMsg := <-operationLogChan:
			fmt.Fprintf(w, "data: %s\n\n", logMsg)
			flusher.Flush()
		}
	}
}

func operationLog(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Println(msg)
	select {
	case operationLogChan <- msg:
	default:
	}
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operationLog("[UPDATE] 开始检查更新...")
	release, err := getLatestRelease()
	if err != nil {
		operationLog("[UPDATE] 获取最新版本失败: %v", err)
		http.Error(w, fmt.Sprintf("Failed to check for updates: %v", err), http.StatusInternalServerError)
		return
	}

	if release.TagName == version {
		operationLog("[UPDATE] 当前已是最新版本")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Already up to date",
		})
		return
	}

	operationLog("[UPDATE] 发现新版本: %s", release.TagName)
	assetURL := ""
	expectedHash := ""
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, "armv7-unknown-linux-gnueabihf") {
			assetURL = asset.URL
			operationLog("[UPDATE] 找到匹配的架构: %s", asset.Name)
			if strings.HasPrefix(asset.Digest, "sha256:") {
				expectedHash = strings.TrimPrefix(asset.Digest, "sha256:")
				operationLog("[UPDATE] 获取到期望的哈希值: %s", expectedHash)
			}
			break
		}
	}

	if assetURL == "" {
		operationLog("[UPDATE] 未找到兼容的二进制文件")
		http.Error(w, "No compatible binary found for your architecture", http.StatusNotFound)
		return
	}

	operationLog("[UPDATE] 开始下载并安装新版本...")
	if err := downloadAndInstallBinary(assetURL, release.TagName, expectedHash); err != nil {
		operationLog("[UPDATE] 更新失败: %v", err)
		http.Error(w, fmt.Sprintf("Failed to update: %v", err), http.StatusInternalServerError)
		return
	}

	operationLog("[UPDATE] 成功更新到版本 %s", release.TagName)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("Successfully updated to version %s", release.TagName),
	})
}

var lastCPUStats struct {
	user   float64
	nice   float64
	system float64
	idle   float64
	time   time.Time
}

func getCPUUsage() string {
	if runtime.GOOS == "linux" {
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

					now := time.Now()
					elapsed := now.Sub(lastCPUStats.time).Seconds()

					if elapsed > 0 && lastCPUStats.time.Unix() > 0 {
						userDelta := user - lastCPUStats.user
						niceDelta := nice - lastCPUStats.nice
						systemDelta := system - lastCPUStats.system
						idleDelta := idle - lastCPUStats.idle
						totalDelta := userDelta + niceDelta + systemDelta + idleDelta

						if totalDelta > 0 {
							usage := (userDelta + niceDelta + systemDelta) / totalDelta * 100
							lastCPUStats = struct {
								user   float64
								nice   float64
								system float64
								idle   float64
								time   time.Time
							}{user, nice, system, idle, now}
							return fmt.Sprintf("%.0f%%", usage)
						}
					}

					lastCPUStats = struct {
						user   float64
						nice   float64
						system float64
						idle   float64
						time   time.Time
					}{user, nice, system, idle, now}
					return "0%"
				}
			}
		}
		return "0%"
	}

	if runtime.GOOS == "darwin" {
		cmd := exec.Command("top", "-l", "1", "-n", "0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "0%"
		}

		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "CPU usage") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					userStr := strings.TrimSuffix(fields[2], "%")
					if user, err := strconv.ParseFloat(userStr, 64); err == nil {
						return fmt.Sprintf("%.0f%%", user)
					}
				}
			}
		}
		return "0%"
	}

	return "N/A"
}

func getMemoryUsage() string {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return "N/A"
		}

		lines := strings.Split(string(data), "\n")
		memTotal := 0.0
		memAvailable := 0.0
		swapTotal := 0.0
		swapFree := 0.0

		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, _ := strconv.ParseFloat(fields[1], 64)
				if strings.HasPrefix(line, "MemTotal:") {
					memTotal = value
				} else if strings.HasPrefix(line, "MemAvailable:") {
					memAvailable = value
				} else if strings.HasPrefix(line, "SwapTotal:") {
					swapTotal = value
				} else if strings.HasPrefix(line, "SwapFree:") {
					swapFree = value
				}
			}
		}

		if memTotal > 0 {
			used := memTotal - memAvailable
			memTotalMB := memTotal / 1024
			usedMB := used / 1024

			if swapTotal > 0 {
				swapUsedMB := (swapTotal - swapFree) / 1024
				swapTotalMB := swapTotal / 1024
				return fmt.Sprintf("%.0fMB / %.0fMB, Swap: %.0fMB / %.0fMB", usedMB, memTotalMB, swapUsedMB, swapTotalMB)
			}
			return fmt.Sprintf("%.0fMB / %.0fMB", usedMB, memTotalMB)
		}
		return "N/A"
	}

	if runtime.GOOS == "darwin" {
		cmd := exec.Command("vm_stat")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "N/A"
		}

		lines := strings.Split(string(output), "\n")
		pageSize := 4096.0
		freePages := 0.0
		inactivePages := 0.0
		activePages := 0.0
		wiredPages := 0.0

		for _, line := range lines {
			if strings.Contains(line, "Pages free:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					freePages, _ = strconv.ParseFloat(strings.TrimSuffix(fields[2], "."), 64)
				}
			} else if strings.Contains(line, "Pages inactive:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					inactivePages, _ = strconv.ParseFloat(strings.TrimSuffix(fields[2], "."), 64)
				}
			} else if strings.Contains(line, "Pages active:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					activePages, _ = strconv.ParseFloat(strings.TrimSuffix(fields[2], "."), 64)
				}
			} else if strings.Contains(line, "Pages wired down:") {
				fields := strings.Fields(line)
				if len(fields) >= 4 {
					wiredPages, _ = strconv.ParseFloat(strings.TrimSuffix(fields[3], "."), 64)
				}
			} else if strings.Contains(line, "page size of") {
				fields := strings.Fields(line)
				for i, field := range fields {
					if field == "of" && i+1 < len(fields) {
						pageSize, _ = strconv.ParseFloat(fields[i+1], 64)
						break
					}
				}
			}
		}

		totalPages := freePages + inactivePages + activePages + wiredPages
		usedPages := activePages + wiredPages

		totalMB := (totalPages * pageSize) / 1024 / 1024
		usedMB := (usedPages * pageSize) / 1024 / 1024

		if totalMB > 0 {
			return fmt.Sprintf("%.0fMB / %.0fMB", usedMB, totalMB)
		}
		return "N/A"
	}

	return "N/A"
}

func getServiceStatus() string {
	cmd := exec.Command("pgrep", "-f", "./zeroclaw")
	if err := cmd.Run(); err != nil {
		return "stopped"
	}
	return "running"
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
	operationLog("[CONFIG] 开始验证配置 (最多重试3次)...")
	for i := 0; i < 3; i++ {
		attempt := i + 1
		operationLog("[CONFIG] 验证尝试 %d/3", attempt)
		cmd := exec.Command("./zeroclaw", "agent", "-m", "Hello, ZeroClaw!")
		output, err := cmd.CombinedOutput()
		if err == nil {
			operationLog("[CONFIG] 配置验证成功")
			return nil
		}

		operationLog("[CONFIG] 验证失败 (尝试 %d/3): %s", attempt, string(output))
		if i < 2 {
			operationLog("[CONFIG] 等待3秒后重试...")
			time.Sleep(3 * time.Second)
		} else {
			operationLog("[CONFIG] 验证失败，已达最大重试次数")
			return fmt.Errorf("validation failed after 3 attempts: %s", string(output))
		}
	}
	return fmt.Errorf("validation failed")
}

func restartService() error {
	cmd := exec.Command("./zeroclaw", "estop", "resume")
	if err := cmd.Run(); err != nil {
		log.Printf("[SERVICE] 停止服务失败: %v", err)
	}
	cmd = exec.Command("./zeroclaw", "daemon")
	return cmd.Run()
}

func getLatestRelease() (*GitHubRelease, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	resp, err := client.Get("https://api.github.com/repos/zeroclaw-labs/zeroclaw/releases/latest")
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

func downloadAndInstallBinary(assetURL, version, expectedHash string) error {
	operationLog("[UPDATE] 正在下载: %s", assetURL)
	resp, err := http.Get(assetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tempFile := fmt.Sprintf("/tmp/zeroclaw-%s-armv7.tar.gz", version)
	operationLog("[UPDATE] 正在保存到临时文件: %s", tempFile)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}
	defer os.Remove(tempFile)
	operationLog("[UPDATE] 下载完成")

	operationLog("[UPDATE] 开始计算下载文件的 SHA256 哈希值...")
	actualHash, err := calculateSHA256(tempFile)
	if err != nil {
		return fmt.Errorf("计算哈希值失败: %v", err)
	}
	operationLog("[UPDATE] 下载文件 SHA256: %s", actualHash)

	if expectedHash != "" {
		operationLog("[UPDATE] 期望的 SHA256: %s", expectedHash)
		operationLog("[UPDATE] 正在对比哈希值...")
		if actualHash != expectedHash {
			operationLog("[UPDATE] 哈希值不匹配！")
			operationLog("[UPDATE]   期望: %s", expectedHash)
			operationLog("[UPDATE]   实际: %s", actualHash)
			return fmt.Errorf("哈希值校验失败：文件可能已损坏或被篡改")
		}
		operationLog("[UPDATE] 哈希值校验通过！")
	} else {
		operationLog("[UPDATE] 警告：未提供期望的哈希值，跳过校验")
	}

	binaryPath := "./zeroclaw"
	backupPath := "./zeroclaw.bak"

	operationLog("[UPDATE] 正在备份当前版本: %s -> %s", binaryPath, backupPath)
	if err := copyFile(binaryPath, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to backup binary: %v", err)
	}
	operationLog("[UPDATE] 备份完成")

	operationLog("[UPDATE] 正在停止服务...")
	cmd := exec.Command("./zeroclaw", "service", "stop")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to stop service: %v", err)
	}

	operationLog("[UPDATE] 正在解压文件...")
	extractedBinary := "/tmp/zeroclaw"
	if _, err := os.Stat(extractedBinary); err == nil {
		operationLog("[UPDATE] 检测到旧的二进制文件，正在删除: %s", extractedBinary)
		if err := os.Remove(extractedBinary); err != nil {
			log.Printf("Warning: failed to remove old binary: %v", err)
		}
	}
	cmd = exec.Command("tar", "-xzf", tempFile, "-C", "/tmp")
	if err := cmd.Run(); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to extract archive: %v", err)
	}
	operationLog("[UPDATE] 解压完成")

	if _, err := os.Stat(extractedBinary); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("extracted binary not found: %v", err)
	}

	operationLog("[UPDATE] 正在复制新版本: %s -> %s", extractedBinary, binaryPath)
	if err := copyFile(extractedBinary, binaryPath); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to copy binary: %v", err)
	}

	operationLog("[UPDATE] 正在设置执行权限...")
	if err := os.Chmod(binaryPath, 0755); err != nil {
		restoreBinary(backupPath)
		return fmt.Errorf("failed to set executable permissions: %v", err)
	}
	operationLog("[UPDATE] 权限设置完成")

	operationLog("[UPDATE] 正在启动服务...")
	operationLog("[UPDATE] 调用后端 /api/service/control 接口，参数 action: start")
	startReq := ServiceControlRequest{Action: "start", Type: "daemon"}
	startReqJSON, _ := json.Marshal(startReq)
	startResp, err := http.Post("http://localhost:42611/api/service/control", "application/json", bytes.NewBuffer(startReqJSON))
	if err != nil {
		log.Printf("Warning: failed to start service via API: %v", err)
	} else {
		startResp.Body.Close()
	}

	operationLog("[UPDATE] 正在检查服务状态...")
	operationLog("[UPDATE] 调用后端 /api/system/status 接口确认服务状态")
	statusResp, err := http.Get("http://localhost:42611/api/system/status")
	if err != nil {
		log.Printf("Warning: failed to check service status via API: %v", err)
	} else {
		defer statusResp.Body.Close()
		var status SystemStatus
		if err := json.NewDecoder(statusResp.Body).Decode(&status); err == nil {
			operationLog("[UPDATE] 服务状态: %s", status.ServiceStatus)
		}
	}

	operationLog("[UPDATE] 更新流程完成")
	return nil
}

func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开文件: %v", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("计算哈希值失败: %v", err)
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	return hash, nil
}

func restoreBinary(backupPath string) {
	if _, err := os.Stat(backupPath); err == nil {
		copyFile(backupPath, "./zeroclaw")
	}
}

func handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("[VERSION] 开始检查版本...")
	response := VersionCheckResponse{}

	localVersion, err := getLocalZeroclawVersion()
	if err != nil {
		log.Printf("[VERSION] 检测本地版本失败: %v", err)
		response.Error = fmt.Sprintf("未检测到 zeroclaw 程序: %v", err)
		response.Message = "请在同级目录放置 zeroclaw 二进制文件"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	log.Printf("[VERSION] 检测到 zeroclaw，版本 %s", localVersion)
	response.LocalVersion = localVersion

	latestRelease, err := getLatestRelease()
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "Timed out") {
			log.Printf("[VERSION] 请求超时，网络错误")
			log.Printf("[VERSION] 云端链接: https://api.github.com/repos/zeroclaw-labs/zeroclaw/releases/latest")
			response.Error = "请求超时，网络错误"
			response.Message = "https://api.github.com/repos/zeroclaw-labs/zeroclaw/releases/latest"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		log.Printf("[VERSION] 获取云端版本失败: %v", err)
		response.Error = fmt.Sprintf("获取云端版本失败: %v", err)
		response.Message = "无法连接到 GitHub"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	log.Printf("[VERSION] 本地版本: %s", localVersion)
	log.Printf("[VERSION] 云端最新版本: %s", latestRelease.TagName)
	response.LatestVersion = latestRelease.TagName
	response.Message = latestRelease.HTMLURL

	if compareVersions(localVersion, latestRelease.TagName) < 0 {
		response.UpdateAvailable = true
		log.Printf("[VERSION] 发现新版本: %s -> %s", localVersion, latestRelease.TagName)
		response.Message = fmt.Sprintf("%s -> %s", localVersion, latestRelease.TagName)
	} else {
		response.UpdateAvailable = false
		log.Printf("[VERSION] 当前已是最新版本: %s", localVersion)
		response.Message = "当前已是最新版本"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getLocalZeroclawVersion() (string, error) {
	execDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("无法获取当前目录: %v", err)
	}

	zeroclawPath := filepath.Join(execDir, "zeroclaw")
	if _, err := os.Stat(zeroclawPath); os.IsNotExist(err) {
		return "", fmt.Errorf("未找到 zeroclaw 二进制文件")
	}

	cmd := exec.Command(zeroclawPath, "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行 zeroclaw -V 失败: %v", err)
	}

	versionStr := strings.TrimSpace(string(output))

	fields := strings.Fields(versionStr)
	if len(fields) >= 2 {
		version := fields[1]
		return version, nil
	}

	return "", fmt.Errorf("无法解析版本号: %s", versionStr)
}

func compareVersions(v1, v2 string) int {
	v1Parts := strings.Split(strings.TrimPrefix(v1, "v"), ".")
	v2Parts := strings.Split(strings.TrimPrefix(v2, "v"), ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var v1Num, v2Num int
		if i < len(v1Parts) {
			v1Num, _ = strconv.Atoi(v1Parts[i])
		}
		if i < len(v2Parts) {
			v2Num, _ = strconv.Atoi(v2Parts[i])
		}

		if v1Num < v2Num {
			return -1
		} else if v1Num > v2Num {
			return 1
		}
	}

	return 0
}

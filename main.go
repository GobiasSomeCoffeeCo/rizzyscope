package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	padding   = 2
	maxWidth  = 80
	timeout   = 5 * time.Second        // Timeout duration for holding RSSI value
	interval  = 500 * time.Millisecond // Query interval
	decayRate = 10                     // Rate at which RSSI decays if no new data
	minRSSI   = -120                   // Minimum RSSI value for progress bar
	maxRSSI   = -20                    // Maximum RSSI value for progress bar
)

var helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render

// Model struct
type model struct {
	progress      progress.Model
	rssi          int
	lastReceived  time.Time // Time when the last RSSI value was received
	mac           []string
	lockedMac     string
	ignoreList    []string
	channel       string
	channelLocked bool
	iface         string
	kismet        *exec.Cmd
}

// Tick message to trigger updates
type tickMsg time.Time

// API response structure
type KismetPayload struct {
	Fields [][]string `json:"fields"`
}

// Function to find a valid MAC from the list of target MACs
func findValidMac(macs []string, ignoreMacs []string) (string, string) {
	postJson := KismetPayload{
		Fields: [][]string{
			{"kismet.device.base.macaddr", "base.macaddr"},
			{"kismet.device.base.channel", "base.channel"},
		},
	}

	jsonData, err := json.Marshal(postJson)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return "", ""
	}

	req, err := createRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println(err)
		return "", ""
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v \n\rplease make sure Kismet is on and running.\n\r", err)
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var devices []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
			fmt.Println("Error decoding response:", err)
			return "", ""
		}

		// Loop through all MAC addresses and find the first valid one
		for _, mac := range macs {
			// Skip ignored MAC addresses
			if contains(ignoreMacs, mac) {
				continue
			}

			// Check if the device matches one of the target MAC addresses
			for _, device := range devices {
				if device["base.macaddr"].(string) == mac {
					// Return the first valid MAC address and its channel
					channel, ok := device["base.channel"].(string)
					if ok {
						return mac, channel
					}
				}
			}
		}
	}
	// Return empty if no valid MAC is found
	return "", ""
}

// Helper function to check if a MAC is in the ignore list
func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// Function to get credentials from configuration
func getCredentials() (string, string, error) {
	user := viper.GetString("credentials.user")
	password := viper.GetString("credentials.password")

	if user == "" || password == "" {
		return "", "", fmt.Errorf("user or password not provided in the configuration")
	}

	return user, password, nil
}

func launchKismet(iface string) (*exec.Cmd, error) {
	fmt.Println("🚀 Launching Kismet...")

	// kismetDevice := viper.GetString("interface")
	cmd := exec.Command("kismet", "-c", iface)

	// Redirecting stdout and stderr to /dev/null to suppress output
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return cmd, fmt.Errorf("failed to start Kismet: %v", err)
	}

	fmt.Println("💥 Launched Kismet")

	return cmd, nil
}

// Function to create an HTTP request with credentials
func createRequest(method, url string, body io.Reader) (*http.Request, error) {
	user, password, err := getCredentials()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	q := req.URL.Query()
	q.Add("user", user)
	q.Add("password", password)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// Function to fetch RSSI and channel data from Kismet
func fetchRSSIData(mac string) (int, string) {
	postJson := KismetPayload{
		Fields: [][]string{
			{"kismet.device.base.macaddr", "base.macaddr"},
			{"kismet.device.base.channel", "base.channel"},
			{"kismet.device.base.signal/kismet.common.signal.last_signal", "RSSI"},
		},
	}

	jsonData, err := json.Marshal(postJson)
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return minRSSI, ""
	}

	req, err := createRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println(err)
		return minRSSI, ""
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v \n\rplease make sure kismet is on and running.\n\r", err)
		return minRSSI, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var devices []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
			fmt.Println("Error decoding response:", err)
			return minRSSI, ""
		}

		for _, device := range devices {
			if device["base.macaddr"].(string) == mac {
				rssi, ok := device["RSSI"].(float64)
				channel, ok2 := device["base.channel"].(string)
				if ok && ok2 {
					return int(rssi), channel
				}
			}
		}
	}
	return minRSSI, ""
}

func getUUIDForInterface(interfaceName string) (string, error) {
	req, err := createRequest("GET", "http://127.0.0.1:2501/datasource/all_sources.json", nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get data sources: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get data sources: %s", string(body))
	}

	body, _ := io.ReadAll(resp.Body)

	var sources []map[string]interface{}
	if err := json.Unmarshal(body, &sources); err != nil {
		return "", fmt.Errorf("failed to decode JSON: %v", err)
	}

	for _, source := range sources {
		if source["kismet.datasource.interface"] == interfaceName {
			if uuid, ok := source["kismet.datasource.uuid"].(string); ok {
				return uuid, nil
			}
		}
	}

	return "", fmt.Errorf("UUID not found for interface %s", interfaceName)
}

func hopChannel(uuid string) error {

	url := fmt.Sprintf("http://127.0.0.1:2501/datasource/by-uuid/%s/set_hop.cmd", uuid)

	req, err := createRequest("POST", url, nil)
	if err != nil {
		fmt.Printf("failed to create request: %v", err)
		return fmt.Errorf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to send request: %v\n", err)
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("failed to unlock channel: %s\n", string(body))
		return fmt.Errorf("failed to unlock channel: %s", string(body))
	}

	fmt.Println("Channel unlock successful.")

	return nil

}

// Function to lock the channel for a specific interface UUID
func lockChannel(uuid, channel string) error {
	url := fmt.Sprintf("http://127.0.0.1:2501/datasource/by-uuid/%s/set_channel.cmd", uuid)

	payload := map[string]string{"channel": channel}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("failed to marshal JSON: %v\n", err)
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	req, err := createRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("failed to create request: %v\n", err)
		return fmt.Errorf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to send request: %v\n", err)
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("failed to lock channel: %s\n", string(body))
		return fmt.Errorf("failed to lock channel: %s", string(body))
	}

	fmt.Println("Channel locked successfully.")

	return nil
}

func (m *model) Init() tea.Cmd {
	return tickCmd()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	uuid, err := getUUIDForInterface(m.iface)
	if err != nil {
		fmt.Println("Failed to get UUID:", err)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q": // Handle Ctrl+C and 'q' to quit
			m.kismet.Process.Kill()
			return m, tea.Quit
		case "i":
			// If a MAC is locked, add it to the ignore list
			if m.lockedMac != "" {
				m.ignoreList = append(m.ignoreList, m.lockedMac) // Add current locked MAC to ignore list
				fmt.Printf("MAC %s added to ignore list\n", m.lockedMac)
				m.lockedMac = "" // Clear the locked MAC
				m.channel = ""
				m.channelLocked = false // Clear the channel
			}
			// Continue channel hopping
			hopChannel(uuid)
			return m, nil
		default:
			// Handle other keys or do nothing
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - padding*2 - 4
		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}
		return m, nil

	case tickMsg:
		if m.lockedMac == "" {
			// Find a valid MAC to lock onto if none is locked
			m.lockedMac, m.channel = findValidMac(m.mac, m.ignoreList)
			m.channelLocked = false // Set this to false because we're locking a new MAC
		}

		newRSSI, newChannel := fetchRSSIData(m.lockedMac)

		if newRSSI != minRSSI && newChannel != "" {
			m.rssi = newRSSI
			m.channel = newChannel
			m.lastReceived = time.Now()

			// Only lock the channel if it's not already locked to the current MAC and channel
			if !m.channelLocked {
				fmt.Printf("Locking MAC %s on channel %s\n", m.lockedMac, newChannel)

				if err := lockChannel(uuid, newChannel); err != nil {
					fmt.Println("Failed to lock channel:", err)
				} else {
					m.channelLocked = true // Set the flag to indicate the channel is now locked
				}
			}
		} else if time.Since(m.lastReceived) > timeout {
			// Decay the RSSI if no new data
			if m.rssi > minRSSI {
				m.rssi -= decayRate
				if m.rssi < minRSSI {
					m.rssi = minRSSI
				}
			}
		}

		percent := float64(m.rssi-minRSSI) / float64(maxRSSI-minRSSI)
		if percent < 0 {
			percent = 0
		} else if percent > 1 {
			percent = 1
		}

		m.progress.SetPercent(percent)

		return m, tea.Batch(tickCmd(), m.progress.IncrPercent(0))

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	default:
		return m, nil
	}
}

func (m *model) View() string {
	pad := strings.Repeat(" ", padding)
	progressBar := m.progress.View()
	rssiString := fmt.Sprintf(" %d dBm", m.rssi)
	fullProgressBar := progressBar + rssiString

	view := "\n" + pad + fullProgressBar + "\n\n"

	if m.lockedMac != "" {
		view += pad + fmt.Sprintf("MAC: %s\n", m.lockedMac)
	}

	if m.channel != "" { // Only add the channel line if it's not empty
		view += pad + fmt.Sprintf("Channel: %s\n", m.channel)
	}

	view += pad + helpStyle("Press 'ctrl+c' to quit\n  Press 'i' to ignore current mac and continue searching")

	return view
}

func tickCmd() tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func main() {

	if os.Geteuid() != 0 {
		fmt.Println("Run as root...")
		os.Exit(1)
	}

	pflag.StringSliceP("mac", "m", []string{}, "MAC address(es) of the device(s), comma-separated")
	pflag.StringP("interface", "i", "", "Interface name")
	pflag.StringP("config", "c", "", "Path to config file")
	pflag.Parse()

	// If no config file is specified via the command line, look for config.toml in the current directory
	configPath := viper.GetString("config")
	if configPath == "" {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
	} else {
		viper.SetConfigFile(configPath)
	}

	// Load the configuration file
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Error reading config file:", err)
		os.Exit(1)
	}

	// After loading the config file, bind the flags to override if provided
	viper.BindPFlag("required.mac", pflag.Lookup("mac"))
	viper.BindPFlag("required.interface", pflag.Lookup("interface"))

	// Retrieve the credentials from the config
	user := viper.GetString("credentials.user")
	password := viper.GetString("credentials.password")

	if user == "" || password == "" {
		fmt.Println("Username or password missing in the configuration")
		os.Exit(1)
	}

	// Initialize the model with the correct configuration paths
	m := model{
		progress:     progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
		rssi:         minRSSI,
		lastReceived: time.Now(),
		mac:          viper.GetStringSlice("required.target_mac"),
		iface:        viper.GetString("required.interface"),
	}

	kismet, err := launchKismet(m.iface)
	if err != nil {
		fmt.Println("Kismet couldn't launch. Check that the interface is correct.")
		os.Exit(1)
	}

	m.kismet = kismet

	time.Sleep(3 * time.Second)

	if _, err := tea.NewProgram(&m).Run(); err != nil {
		fmt.Println("Oh no!", err)
		os.Exit(1)
	}
}

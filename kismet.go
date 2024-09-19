package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"

	"github.com/spf13/viper"
)

// API response structure
type KismetPayload struct {
	Fields [][]string `json:"fields"`
}

// Function to find a valid MAC from the list of target MACs
func FindValidMac(macs []string, ignoreMacs []string) (string, string) {
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

	req, err := CreateRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
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

			for _, device := range devices {
				if device["base.macaddr"].(string) == mac {
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

func LaunchKismet(iface string) (*exec.Cmd, error) {
	fmt.Println("🚀 Launching Kismet...")

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
func CreateRequest(method, url string, body io.Reader) (*http.Request, error) {
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
func FetchRSSIData(mac string) (int, string) {
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

	req, err := CreateRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
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

func GetUUIDForInterface(interfaceName string) (string, error) {
	req, err := CreateRequest("GET", "http://127.0.0.1:2501/datasource/all_sources.json", nil)
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

	req, err := CreateRequest("POST", url, nil)
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

	req, err := CreateRequest("POST", url, bytes.NewBuffer(jsonData))
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

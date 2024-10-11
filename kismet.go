package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/spf13/viper"
)

const (
	MinRSSI = -120 // Minimum RSSI value for progress bar
	MaxRSSI = -20  // Maximum RSSI value for progress bar

)

var (
	cachedUser        string
	cachedPassword    string
	credentialsErr    error
	once              sync.Once                        // Ensures credentials are fetched only once
	errDeviceNotFound = errors.New("device not found") // Error to match on
)

type DeviceInfo struct {
	RSSI              int               // Signal strength
	Channel           string            // Operating channel
	Manufacturer      string            // Manufacturer of the device
	SSID              string            // SSID of the device (if applicable)
	Crypt             string            // Encryption type
	Type              string            // Device type (AP, Client, etc.)
	AssociatedClients map[string]string // Map of associated client MAC addresses
}

// API response structure
type KismetPayload struct {
	Fields [][]string `json:"fields"`
}

// Function to fetch device info from Kismet
func FetchDeviceInfo(mac string) (*DeviceInfo, error) {
	postJson := KismetPayload{
		Fields: [][]string{
			{"kismet.device.base.macaddr", "base.macaddr"},
			{"kismet.device.base.channel", "base.channel"},
			{"kismet.device.base.signal/kismet.common.signal.last_signal", "RSSI"},
			{"kismet.device.base.manuf", "Make"},
			{"dot11.device/dot11.device.last_beaconed_ssid_record/dot11.advertisedssid.ssid", "SSID"},
			{"kismet.device.base.crypt", "Crypt"},
			{"kismet.device.base.type", "Type"},
			{"dot11.device/dot11.device.associated_client_map", "AssociatedClients"},
		},
	}

	jsonData, err := json.Marshal(postJson)
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return nil, err
	}

	req, err := CreateRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// Log the error but do not return it to the user
		log.Printf("Error making request to Kismet API: %v", err)
		return nil, nil // Return nil to indicate no data but no critical error
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var devices []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
			log.Printf("Error decoding response: %v", err)
			return nil, err
		}

		for _, device := range devices {
			// Check if the MAC address matches
			if macAddr, ok := device["base.macaddr"].(string); ok && macAddr == mac {
				deviceInfo := &DeviceInfo{
					RSSI:              MinRSSI, // Default RSSI value
					Channel:           "",
					Manufacturer:      "Unknown",
					SSID:              "Unknown",
					Crypt:             "Unknown",
					Type:              "Unknown",
					AssociatedClients: map[string]string{},
				}

				// Extract fields
				if rssiVal, ok := device["RSSI"].(float64); ok {
					deviceInfo.RSSI = int(rssiVal)
				}
				if channelVal, ok := device["base.channel"].(string); ok {
					deviceInfo.Channel = channelVal
				}
				if makeVal, ok := device["Make"].(string); ok {
					deviceInfo.Manufacturer = makeVal
				}
				if ssidVal, ok := device["SSID"].(string); ok {
					deviceInfo.SSID = ssidVal
				}
				if cryptVal, ok := device["Crypt"].(string); ok {
					deviceInfo.Crypt = cryptVal
				}
				if typeVal, ok := device["Type"].(string); ok {
					deviceInfo.Type = typeVal
				}
				// Extract associated clients (if any)
				if associatedClientsVal, ok := device["AssociatedClients"].(map[string]interface{}); ok {
					for clientMac, assoc := range associatedClientsVal {
						deviceInfo.AssociatedClients[clientMac] = fmt.Sprintf("%v", assoc)
					}
				}

				return deviceInfo, nil
			}
		}
	}

	return nil, errDeviceNotFound
}

func FindValidTarget(targets []*TargetItem) (string, string, *TargetItem, error) {
	// Prepare the payload for Kismet API request
	postJson := KismetPayload{
		Fields: [][]string{
			{"kismet.device.base.macaddr", "base.macaddr"},
			{"kismet.device.base.channel", "base.channel"},
			{"dot11.device/dot11.device.last_beaconed_ssid_record/dot11.advertisedssid.ssid", "SSID"},
		},
	}

	// Marshal the payload to JSON
	jsonData, err := json.Marshal(postJson)
	if err != nil {
		return "", "", nil, fmt.Errorf("error marshaling JSON: %v", err)
	}

	// Create the HTTP POST request
	req, err := CreateRequest("POST", "http://127.0.0.1:2501/devices/last-time/-5/devices.json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", nil, fmt.Errorf("error creating request: %v", err)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("error making request to Kismet API: %v", err)
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("kismet API returned status code %d", resp.StatusCode)
	}

	// Decode the response body
	var devices []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return "", "", nil, fmt.Errorf("error decoding response: %v", err)
	}

	// Iterate over targets
	for _, target := range targets {
		if target.IsIgnored() {
			continue
		}

		// Iterate over devices
		for _, device := range devices {
			// Extract device fields
			deviceMac, _ := device["base.macaddr"].(string)
			deviceChannel, _ := device["base.channel"].(string)
			// deviceSSID, _ := device["SSID"].(string)

			if target.TType == MAC {
				if deviceMac == target.Value {
					return target.Value, deviceChannel, target, nil
				}
			} else if target.TType == SSID {
				if ssidVal, ok := device["SSID"].(string); ok && ssidVal == target.Value {
					macAddr, _ := device["base.macaddr"].(string)
					channel, ok := device["base.channel"].(string)
					if ok {
						newTarget := target                    // Create a copy of the target
						newTarget.OriginalValue = target.Value // Store the original SSID
						newTarget.TType = SSID
						newTarget.Value = macAddr // Set the value to the MAC address
						return macAddr, channel, newTarget, nil
					}
				}
			}
		}
	}

	// No valid target found
	return "", "", nil, nil
}

// Function to lazily pull credentials and store them in global variables so we're not unnecessarily pulling them for every api query.
func getCachedCredentials() (string, string, error) {
	once.Do(func() {
		cachedUser, cachedPassword, credentialsErr = getCredentials()
	})
	return cachedUser, cachedPassword, credentialsErr
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

func LaunchKismet(ifaces []string) (*exec.Cmd, error) {
	log.Println("Launching Kismet...")

	// Initialize the arguments with kismet command
	args := []string{}

	// Add -c before each interface
	for _, iface := range ifaces {
		args = append(args, "-c", iface)
	}

	// Create the command with the dynamically built args
	cmd := exec.Command("kismet", args...)

	// Redirecting stdout and stderr to /dev/null to suppress output
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return cmd, fmt.Errorf("failed to start Kismet: %v", err)
	}

	log.Println("Kismet launched successfully")

	return cmd, nil
}

// Function to create an HTTP request with credentials
func CreateRequest(method, url string, body io.Reader) (*http.Request, error) {
	user, password, err := getCachedCredentials()
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

// Function to get UUID for a specific interface
func GetUUIDForInterface(interfaceName string) (string, error) {
	req, err := CreateRequest("GET", "http://127.0.0.1:2501/datasource/all_sources.json", nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error getting data sources: %v", err)
		return "", fmt.Errorf("failed to get data sources: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to get data sources: %s", string(body))
		return "", fmt.Errorf("failed to get data sources: %s", string(body))
	}

	body, _ := io.ReadAll(resp.Body)

	var sources []map[string]interface{}
	if err := json.Unmarshal(body, &sources); err != nil {
		log.Printf("Error decoding JSON: %v", err)
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
		log.Printf("Failed to create request: %v", err)
		return fmt.Errorf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to send request: %v", err)
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to unlock channel: %s", string(body))
		return fmt.Errorf("failed to unlock channel: %s", string(body))
	}

	return nil
}

// Function to lock the channel for a specific interface UUID
func lockChannel(uuid, channel string) error {
	url := fmt.Sprintf("http://127.0.0.1:2501/datasource/by-uuid/%s/set_channel.cmd", uuid)

	payload := map[string]string{"channel": channel}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal JSON: %v", err)
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	req, err := CreateRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return fmt.Errorf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to send request: %v", err)
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to lock channel: %s", string(body))
		return fmt.Errorf("failed to lock channel: %s", string(body))
	}

	return nil
}

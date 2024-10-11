package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func formatMAC(mac string) (string, error) {
	// Remove all non-hexadecimal characters
	cleanMAC := regexp.MustCompile(`[^0-9A-Fa-f]`).ReplaceAllString(mac, "")

	// Check if the cleaned MAC has exactly 12 characters
	if len(cleanMAC) != 12 {
		return "", fmt.Errorf("invalid MAC address: %s", mac)
	}

	// Format the MAC address
	formattedMAC := strings.ToUpper(fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		cleanMAC[0:2], cleanMAC[2:4], cleanMAC[4:6],
		cleanMAC[6:8], cleanMAC[8:10], cleanMAC[10:12]))

	return formattedMAC, nil
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("Run as root...")
		os.Exit(1)
	}

	pflag.StringSliceP("mac", "m", []string{}, "MAC address(es) of the device(s)")
	pflag.StringSliceP("ssid", "s", []string{}, "SSID of the device(s)")
	pflag.StringSliceP("interface", "i", []string{}, "Interface name")
	pflag.StringP("config", "c", "", "Path to config file")
	pflag.Parse()

	configPath := viper.GetString("config")
	if configPath == "" {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
	} else {
		viper.SetConfigFile(configPath)
	}

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Error reading config file:", err)
		os.Exit(1)
	}

	viper.BindPFlag("required.target_mac", pflag.Lookup("mac"))
	viper.BindPFlag("required.interface", pflag.Lookup("interface"))
	viper.BindPFlag("optional.target_ssid", pflag.Lookup("ssid"))

	// Read MACs and SSIDs from Viper
	rawTargetMACs := viper.GetStringSlice("required.target_mac")
	targetSSIDs := viper.GetStringSlice("optional.target_ssid")

	// Format and validate MAC addresses
	var targetMACs []string
	for _, mac := range rawTargetMACs {
		formattedMAC, err := formatMAC(mac)
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			continue
		}
		targetMACs = append(targetMACs, formattedMAC)
	}

	// Build the targets slice
	var targets []*TargetItem
	for _, mac := range targetMACs {
		targets = append(targets, &TargetItem{Value: mac, TType: MAC})
	}
	for _, ssid := range targetSSIDs {
		targets = append(targets, &TargetItem{Value: ssid, TType: SSID})
	}

	m := Model{
		progress:      progress.New(progress.WithGradient("#ff5555", "#50fa7b"), progress.WithoutPercentage()),
		rssi:          MinRSSI,
		lastReceived:  time.Now(),
		targets:       targets,
		iface:         viper.GetStringSlice("required.interface"),
		realTimeOutput: []string{},
		ignoreList:    []string{},
		windowWidth:   80,
		targetList:    list.New([]list.Item{}, list.NewDefaultDelegate(), 40, 10),
	}

	kismet, err := LaunchKismet(m.iface)
	if err != nil {
		fmt.Println("Kismet couldn't launch. Check the interface.")
		os.Exit(1)
	}
	m.kismet = kismet

	time.Sleep(3 * time.Second)

	if _, err := tea.NewProgram(&m).Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
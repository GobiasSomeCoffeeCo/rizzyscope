package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Clear the terminal screen
func clearScreen() {
    cmd := exec.Command("clear") // For Linux/Mac
    cmd.Stdout = os.Stdout
    cmd.Run()
}

func formatMAC(mac string) (string, error) {
	cleanMAC := regexp.MustCompile(`[^0-9A-Fa-f]`).ReplaceAllString(mac, "")

	if len(cleanMAC) != 12 {
		return "", fmt.Errorf("invalid MAC address: %s", mac)
	}

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
	pflag.StringP("kismet-endpoint", "u", "127.0.0.1:2501", "Kismet server endpoint ip:port")
	skipKismet := pflag.BoolP("skip-kismet", "k", false, "Skip launching Kismet (use if kismet is already running)")
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

	if err := viper.BindPFlag("required.target_mac", pflag.Lookup("mac")); err != nil {
		log.Printf("Error in parsing MAC flag/config: %v", err)
	}

	if err := viper.BindPFlag("required.interface", pflag.Lookup("interface")); err != nil {
		log.Printf("Error in parsing interface flag/config: %v", err)
	}

	if err := viper.BindPFlag("optional.kismet_endpoint", pflag.Lookup("kismet-endpoint")); err != nil {
		log.Printf("Error in parsing kismet-endpoint flag/config: %v", err)
	}

	if err := viper.BindPFlag("optional.target_ssid", pflag.Lookup("ssid")); err != nil {
		log.Printf("Error in parsing 'ssid' flag/config: %v", err)
	}

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
		progress:       progress.New(progress.WithGradient("#ff5555", "#50fa7b"), progress.WithoutPercentage()),
		rssi:           MinRSSI,
		lastReceived:   time.Now(),
		targets:        targets,
		iface:          viper.GetStringSlice("required.interface"),
		realTimeOutput: []string{},
		ignoreList:     []string{},
		windowWidth:    80,
		targetList:     list.New([]list.Item{}, list.NewDefaultDelegate(), 40, 10),
		kismetEndpoint: viper.GetString("optional.kismet_endpoint"),
		kismetData:     make([]string, 0),
		maxDataSize:    10,
	}

	if *skipKismet {
		m.kismet = nil
	} else {
		kismet, err := LaunchKismet(m.iface)
		if err != nil {
			fmt.Println("Kismet couldn't launch. Please ensure Kimset is installed and in your $PATH.")
			os.Exit(1)
		}

		m.kismet = kismet
	}

	time.Sleep(3 * time.Second)
	clearScreen()

	if _, err := tea.NewProgram(&m).Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

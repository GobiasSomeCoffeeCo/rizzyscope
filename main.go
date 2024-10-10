package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("Run as root...")
		os.Exit(1)
	}

	pflag.StringSliceP("mac", "m", []string{}, "MAC address(es) of the device(s)")
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

	m := Model{
		progress:       progress.New(progress.WithGradient("#ff5555", "#50fa7b"), progress.WithoutPercentage()),
		rssi:           minRSSI,
		lastReceived:   time.Now(),
		targetMACs:     viper.GetStringSlice("required.target_mac"),
		iface:          viper.GetStringSlice("required.interface"),
		realTimeOutput: []string{},
		windowWidth:    80,
		macList:        list.New([]list.Item{}, list.NewDefaultDelegate(), 40, 10),
		sineTick:       0,
		amplitude:      8, // Adjust amplitude
		frequency:      0.1,
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

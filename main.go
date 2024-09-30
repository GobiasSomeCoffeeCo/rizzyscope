package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
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
)

type tickMsg time.Time

type MACItem struct {
	mac    string
	locked bool
}

func (i MACItem) Title() string       { return i.mac }
func (i MACItem) Description() string { return "" }
func (i MACItem) FilterValue() string { return i.mac }

type Model struct {
	progress       progress.Model
	rssi           int
	targetMACs     []string
	lockedMac      string
	channel        string
	ignoreList     []string
	iface          string
	lastReceived   time.Time
	kismet         *exec.Cmd
	channelLocked  bool
	realTimeOutput []string
	windowWidth    int
	macList        list.Model
}

func (m *Model) Init() tea.Cmd {
	return tickCmd()
}

// Add a message to the real-time output, ensuring we only keep the last 25 messages
func (m *Model) addRealTimeOutput(message string) {
	m.realTimeOutput = append(m.realTimeOutput, message)
	if len(m.realTimeOutput) > 7 {
		m.realTimeOutput = m.realTimeOutput[len(m.realTimeOutput)-7:]
	}
}

// Checks if a MAC is in the ignore list
func (m *Model) isIgnored(mac string) bool {
	for _, ignoredMac := range m.ignoreList {
		if ignoredMac == mac {
			return true
		}
	}
	return false
}

// Removes a MAC from the ignore list
func (m *Model) removeFromIgnoreList(mac string) {
	newList := []string{}
	for _, ignoredMac := range m.ignoreList {
		if ignoredMac != mac {
			newList = append(newList, ignoredMac)
		}
	}
	m.ignoreList = newList
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	uuid, err := GetUUIDForInterface(m.iface)
	if err != nil {
		log.Printf("Failed to get UUID: %v", err)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.kismet.Process.Kill()
			return m, tea.Quit
		case "up", "k", "down", "j":
			var cmd tea.Cmd
			m.macList, cmd = m.macList.Update(msg)
			return m, cmd
		case "enter":
			if selectedItem, ok := m.macList.SelectedItem().(MACItem); ok {
				// Check if the selected MAC is in the ignore list
				if m.isIgnored(selectedItem.mac) {
					// Remove from ignore list
					m.removeFromIgnoreList(selectedItem.mac)
					m.addRealTimeOutput(fmt.Sprintf("MAC %s removed from ignore list.", selectedItem.mac))
				}

				m.lockedMac = selectedItem.mac
				m.channelLocked = false

				err := hopChannel(uuid)
				if err != nil {
					log.Printf("Error hopping channel: %v", err)
				}

				m.addRealTimeOutput(fmt.Sprintf("Searching for MAC %s...", selectedItem.mac))
			}
			return m, nil
		case "i":
			if m.lockedMac != "" {
				m.ignoreList = append(m.ignoreList, m.lockedMac)
				m.addRealTimeOutput(fmt.Sprintf("MAC %s added to ignore list", m.lockedMac))
				m.lockedMac = ""
				m.channel = ""
				m.addRealTimeOutput("Continuing search for new target MAC...")
				m.channelLocked = false
			}
			err := hopChannel(uuid)
			if err != nil {
				log.Printf("Error hopping channel: %v", err)
			}
			return m, nil
		default:
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.progress.Width = msg.Width/2 - padding*2 - 4
		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}
		m.macList.SetWidth(m.windowWidth / 2)
		return m, nil

	case tickMsg:
		if m.lockedMac == "" {
			m.lockedMac, m.channel = FindValidMac(m.targetMACs, m.ignoreList)
			m.channelLocked = false
			
		}

		if m.lockedMac != "" {
			// Fetch dynamic info periodically
			deviceInfo, err := FetchDeviceInfo(m.lockedMac)
			if err != nil && err != errDeviceNotFound {
				log.Printf("Error fetching device info: %v", err)
			}
			if deviceInfo != nil {
				m.rssi = deviceInfo.RSSI
				m.channel = deviceInfo.Channel
				m.lastReceived = time.Now()

				// Lock the channel if not already locked
				if !m.channelLocked {
					if err := lockChannel(uuid, m.channel); err != nil {
						m.addRealTimeOutput(fmt.Sprintf("Failed to lock channel: %v", err))
					} else {
						m.channelLocked = true
						m.addRealTimeOutput(fmt.Sprintf("Locked MAC %s on channel %s", m.lockedMac, m.channel))
						// m.addRealTimeOutput(fmt.Sprintf("Locked MAC %s", m.lockedMac))
						m.addRealTimeOutput(fmt.Sprintf("Make: %s", deviceInfo.Manufacturer))
						m.addRealTimeOutput(fmt.Sprintf("SSID: %s", deviceInfo.SSID))
						m.addRealTimeOutput(fmt.Sprintf("Encryption: %s", deviceInfo.Crypt))
						m.addRealTimeOutput(fmt.Sprintf("Type: %s", deviceInfo.Type))

						// if len(deviceInfo.AssociatedClients) > 0 {
						// 	for clientMac := range deviceInfo.AssociatedClients {
						// 		m.addRealTimeOutput(fmt.Sprintf("Associated Client: %s", clientMac))
						// 	}
						// }
					}
				}
			}
		}

		// Decay RSSI if no signal received in a while
		if time.Since(m.lastReceived) > timeout && m.rssi > minRSSI {
			m.rssi -= decayRate
			if m.rssi < minRSSI {
				m.rssi = minRSSI
			}
		}

		// Update progress bar
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

func (m *Model) View() string {
	// Calculate the widths for the top two panes (50/50 split)
	topPaneWidth := m.windowWidth / 2

	topLeft := m.renderMacListWithHelp(topPaneWidth)

	topRight := m.renderRSSIProgressBar(topPaneWidth)

	var bottom string

	if m.lockedMac == "" && !m.channelLocked {
		bottom = renderRealTimePane("Searching for target MAC(s)...", m.realTimeOutput, m.windowWidth)
	} else {
		bottom = renderRealTimePane(fmt.Sprintf("Locked to target: %s",m.lockedMac), m.realTimeOutput, m.windowWidth)
	}

	m.macList.SetShowHelp(false)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)
	view := lipgloss.JoinVertical(lipgloss.Top, topRow, bottom)

	return view
}

// Render MAC list pane with custom help text
func (m *Model) renderMacListWithHelp(width int) string {
	listTitle := "Target MACs"

	// Populate the MAC list with items
	var macItems []list.Item
	for _, mac := range m.targetMACs {
		macItems = append(macItems, MACItem{mac: mac, locked: mac == m.lockedMac})
	}

	// Set the list model's items
	m.macList.SetItems(macItems)

	// Render the MAC list and custom help text
	macListView := m.macList.View()
	customHelp := renderCustomHelpText()

	// Create styled header and combine it with the MAC list and custom help
	header := lipgloss.NewStyle().Bold(true).Render(listTitle)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width).
		Render(header + "\n" + macListView + "\n\n" + customHelp)
}

// Render custom help text
func renderCustomHelpText() string {
	help := `
↑/k up • ↓/j down 
[Enter] Search for target MAC
[i] Ignore current target 
[q/Ctrl+C] Quit`
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		Render(help)
}

func (m *Model) renderRSSIProgressBar(width int) string {
	rssiLabel := fmt.Sprintf("RSSI: %d dBm", m.rssi)
	progressBar := m.progress.View()

	rssiDisplay := fmt.Sprintf("%s\n%s", rssiLabel, progressBar)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4).
		Render(rssiDisplay)
}

// Render the real-time output pane with the last entries
func renderRealTimePane(title string, outputs []string, width int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4)

	header := lipgloss.NewStyle().Bold(true).Render(title)
	body := lipgloss.NewStyle().Render(strings.Join(outputs, "\n"))

	return style.Render(header + "\n" + body)
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

	pflag.StringSliceP("mac", "m", []string{}, "MAC address(es) of the device(s)")
	pflag.StringP("interface", "i", "", "Interface name")
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
		iface:          viper.GetString("required.interface"),
		realTimeOutput: []string{},
		windowWidth:    80,
		macList:        list.New([]list.Item{}, list.NewDefaultDelegate(), 40, 10),
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

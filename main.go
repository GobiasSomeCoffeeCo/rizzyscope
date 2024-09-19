package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"os/exec"
	"time"
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

//var helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render

type tickMsg time.Time

type MACItem struct {
	mac    string
	locked bool
}

func (i MACItem) Title() string       { return i.mac }
func (i MACItem) Description() string { return "" }
func (i MACItem) FilterValue() string { return i.mac }

type model struct {
	progress       progress.Model
	rssi           int
	mac            []string
	lockedMac      string
	channel        string
	ignoreList     []string
	iface          string
	lastReceived   time.Time
	kismet         *exec.Cmd
	channelLocked  bool
	realTimeOutput string
	windowWidth    int
	macList        list.Model
}

func (m *model) Init() tea.Cmd {
	return tickCmd()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	uuid, err := GetUUIDForInterface(m.iface)
	if err != nil {
		fmt.Println("Failed to get UUID:", err)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.kismet.Process.Kill()
			return m, tea.Quit
		case "up", "k":

			var cmd tea.Cmd
			m.macList, cmd = m.macList.Update(msg)
			return m, cmd
		case "down", "j":

			var cmd tea.Cmd
			m.macList, cmd = m.macList.Update(msg)
			return m, cmd
		case "enter":

			if selectedItem, ok := m.macList.SelectedItem().(MACItem); ok {
				m.realTimeOutput = fmt.Sprintf("Switching to MAC: %s\n", selectedItem.mac)

				m.lockedMac = selectedItem.mac
				m.channelLocked = false

				hopChannel(uuid)

				m.realTimeOutput = fmt.Sprintf("Searching for MAC %s...\n", selectedItem.mac)
			}
			return m, nil
		case "i":

			if m.lockedMac != "" {
				m.ignoreList = append(m.ignoreList, m.lockedMac)
				m.realTimeOutput = fmt.Sprintf("MAC %s added to ignore list\n", m.lockedMac)
				m.lockedMac = ""
				m.channel = ""
				m.realTimeOutput = fmt.Sprintln("Continuing search for new target MAC...")
				m.channelLocked = false
			}
			hopChannel(uuid)
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
		m.macList.SetWidth(m.windowWidth/2 - 2)
		return m, nil

	case tickMsg:
		if m.lockedMac == "" {
			m.lockedMac, m.channel = FindValidMac(m.mac, m.ignoreList)
			m.channelLocked = false
		}

		newRSSI, newChannel := FetchRSSIData(m.lockedMac)

		if newRSSI != minRSSI && newChannel != "" {
			m.rssi = newRSSI
			m.channel = newChannel
			m.lastReceived = time.Now()

			if !m.channelLocked {
				m.realTimeOutput = fmt.Sprintf("Locking MAC %s on channel %s\n", m.lockedMac, newChannel)

				if err := lockChannel(uuid, newChannel); err != nil {
					fmt.Println("Failed to lock channel:", err)
				} else {
					m.channelLocked = true
					m.realTimeOutput = fmt.Sprintf("Locked MAC %s on channel %s\n", m.lockedMac, newChannel)
				}
			}
		} else if time.Since(m.lastReceived) > timeout {
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
	// Calculate the widths for the top two panes (50/50 split)
	topPaneWidth := m.windowWidth / 2

	topLeft := m.renderMacListWithHelp(topPaneWidth)

	topRight := m.renderRSSIProgressBar(topPaneWidth)

	bottom := renderPane("Real-Time Output", m.realTimeOutput, m.windowWidth)

	m.macList.SetShowHelp(false)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)
	view := lipgloss.JoinVertical(lipgloss.Top, topRow, bottom)

	return view
}

// Render MAC list pane with custom help text
func (m *model) renderMacListWithHelp(width int) string {
	listTitle := "Target MACs"

	// Populate the MAC list with items
	var macItems []list.Item
	for _, mac := range m.mac {
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
		Foreground(lipgloss.Color("#626262")). // Customize help color
		Render(help)
}

func (m *model) renderRSSIProgressBar(width int) string {
	rssiLabel := fmt.Sprintf("RSSI: %d dBm", m.rssi)
	progressBar := m.progress.View()

	rssiDisplay := fmt.Sprintf("%s\n%s", rssiLabel, progressBar)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width).
		Render(rssiDisplay)
}

func renderPane(title, content string, width int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width)

	header := lipgloss.NewStyle().Bold(true).Render(title)
	body := lipgloss.NewStyle().Render(content)

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

	viper.BindPFlag("required.mac", pflag.Lookup("mac"))
	viper.BindPFlag("required.interface", pflag.Lookup("interface"))

	m := model{
		progress:       progress.New(progress.WithGradient("#ff5555", "#50fa7b"), progress.WithoutPercentage()),
		rssi:           minRSSI,
		lastReceived:   time.Now(),
		mac:            viper.GetStringSlice("required.target_mac"),
		iface:          viper.GetString("required.interface"),
		realTimeOutput: "",
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

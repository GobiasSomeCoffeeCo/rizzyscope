package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	rssiData       []int
	targetMACs     []string
	lockedMac      string
	channel        string
	ignoreList     []string
	iface          []string
	lastReceived   time.Time
	kismet         *exec.Cmd
	channelLocked  bool
	realTimeOutput []string
	windowWidth    int
	macList        list.Model
	sineTick       int
	amplitude      int
	frequency      float64
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
	uuid, err := GetUUIDForInterface(m.iface[0])
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
				m.rssiData = append(m.rssiData, m.rssi)
				if len(m.rssiData) > 50 { // Keep only the last 50 data points
					m.rssiData = m.rssiData[1:]
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

	topPaneWidth := m.windowWidth / 2

	topLeft := m.renderMacListWithHelp(topPaneWidth)

	topRight := lipgloss.JoinVertical(
		lipgloss.Top,
		m.renderRSSIProgressBar(topPaneWidth),
		m.renderRSSIOverTimeChart(topPaneWidth),
	)

	var bottomLeft string
	if m.lockedMac == "" && !m.channelLocked {
		bottomLeft = renderRealTimePane("Searching for target MAC(s)...", m.realTimeOutput, topPaneWidth)
	} else {
		bottomLeft = renderRealTimePane(fmt.Sprintf("Locked to target: %s", m.lockedMac), m.realTimeOutput, topPaneWidth)
	}

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)

	return lipgloss.JoinVertical(lipgloss.Top, topRow, bottomLeft)
}

func (m *Model) renderRSSIOverTimeChart(width int) string {
	var builder strings.Builder

	maxRSSI, minRSSI := -30, -120
	height := 7

	// Adjust maxPoints to account for the left wall and make sure the dots don't disappear prematurely
	maxPoints := width - 20 // Adjust the available width to fit properly
	if maxPoints < 0 {
		maxPoints = 0
	}

	// Top border of the chart
	builder.WriteString("     ┌")
	builder.WriteString(strings.Repeat("─", maxPoints))
	builder.WriteString("┐\n")

	// Iterate over each Y-axis level (representing RSSI levels)
	for y := height; y >= 0; y-- {
		rssiLevel := minRSSI + (y * (maxRSSI - minRSSI) / height)

		// Y-axis labels with 4-character padding to ensure vertical bar alignment
		builder.WriteString(fmt.Sprintf("%4d │", rssiLevel))

		// Create an empty row of spaces for this level
		line := make([]rune, maxPoints)
		for i := range line {
			line[i] = ' '
		}

		// Fill in RSSI data from right to left
		for i := 0; i < len(m.rssiData) && i < maxPoints; i++ {
			dataIdx := len(m.rssiData) - (i + 1) // Start from the end of the data
			rssi := m.rssiData[dataIdx]

			normalizedRSSI := (rssi - minRSSI) * height / (maxRSSI - minRSSI)

			if normalizedRSSI == y {
				// Place the dot on the exact level
				line[maxPoints-i-1] = '.'
			} else if normalizedRSSI > y && normalizedRSSI < y+1 {
				// Close to the next level
				line[maxPoints-i-1] = '.'
			} else if normalizedRSSI < y && normalizedRSSI > y-1 {
				// Close to the previous level
				line[maxPoints-i-1] = '.'
			}
		}

		builder.WriteString(string(line))
		builder.WriteString("│\n")
	}

	builder.WriteString("     └ Time ←  ")
	builder.WriteString(strings.Repeat("─", maxPoints-9))
	builder.WriteString("┘\n")

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4).
		Render(builder.String())
}

// Render MAC list pane with custom help text
func (m *Model) renderMacListWithHelp(width int) string {
	listTitle := "Target MACs"

	var macItems []list.Item
	for _, mac := range m.targetMACs {
		macItems = append(macItems, MACItem{mac: mac, locked: mac == m.lockedMac})
	}

	m.macList.SetItems(macItems)

	macListView := m.macList.View()
	m.macList.SetShowHelp(false)
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
		Width(width)

	header := lipgloss.NewStyle().Bold(true).Render(title)
	body := lipgloss.NewStyle().Render(strings.Join(outputs, "\n"))

	return style.Render(header + "\n" + body)
}

func tickCmd() tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

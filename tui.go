package main

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
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

type Model struct {
	progress         progress.Model
	rssi             int
	rssiData         []int
	lockedTarget     *TargetItem
	channel          string
	ignoreList       []string
	iface            []string
	lastReceived     time.Time
	kismet           *exec.Cmd
	targets          []*TargetItem
	channelLocked    bool
	realTimeOutput   []string
	windowWidth      int
	targetList       list.Model
	kismetEndpoint   string
	kismetData       []string // Holds Kismet data to display
	maxDataSize      int
	lockedDeviceInfo *DeviceInfo // Current device info for locked target
	clientScrollOffset int        // Scroll offset for client list
	focusOnClients   bool         // Whether focus is on client list for scrolling
	tempMessages     []string     // Temporary messages that disappear
	tempMsgTimer     time.Time    // Timer for temp messages
}

func (m *Model) Init() tea.Cmd {
	return tickCmd()
}

// Add a message to the real-time output, ensuring we only keep the last 7 messages
func (m *Model) addRealTimeOutput(message string) {
	m.realTimeOutput = append(m.realTimeOutput, message)
	if len(m.realTimeOutput) > 7 {
		m.realTimeOutput = m.realTimeOutput[len(m.realTimeOutput)-7:]
	}
}

// Add a temporary message that will disappear after a few seconds
func (m *Model) addTempMessage(message string) {
	m.tempMessages = append(m.tempMessages, message)
	m.tempMsgTimer = time.Now()
	if len(m.tempMessages) > 3 {
		m.tempMessages = m.tempMessages[len(m.tempMessages)-3:]
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// TODO will need to handle multiple interfaces and bands they can support.
	// The interface chosen has no logic behind whether it can support the channel passed by another network card
	uuid, err := GetUUIDForInterface(m.iface[0], m.kismetEndpoint)
	if err != nil {
		log.Printf("Failed to get UUID: %v\n\rPlease check the config.toml and make sure your interface names are correct.", err)
		if m.kismet != nil {
			if killErr := m.kismet.Process.Kill(); killErr != nil {
				log.Printf("Unable to kill Kismet process: %v", killErr)
			}
		}
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.kismet != nil {
				err := m.kismet.Process.Kill()
				if err != nil {
					log.Printf("Unable to kill Kismet process. Please check if Kismet is still running.")
				}
			}
			return m, tea.Quit
		case "tab":
			// Toggle focus between target list and client list
			if m.lockedTarget != nil && m.lockedDeviceInfo != nil && len(m.lockedDeviceInfo.AssociatedClients) > 0 {
				m.focusOnClients = !m.focusOnClients
			}
			return m, nil
		case "up", "k":
			if m.focusOnClients && m.lockedTarget != nil && m.lockedDeviceInfo != nil && len(m.lockedDeviceInfo.AssociatedClients) > 0 {
				// Scroll up in client list
				if m.clientScrollOffset > 0 {
					m.clientScrollOffset--
				}
				return m, nil
			} else {
				// Normal target list navigation
				var cmd tea.Cmd
				m.targetList, cmd = m.targetList.Update(msg)
				return m, cmd
			}
		case "down", "j":
			if m.focusOnClients && m.lockedTarget != nil && m.lockedDeviceInfo != nil && len(m.lockedDeviceInfo.AssociatedClients) > 0 {
				// Scroll down in client list
				maxVisibleClients := 8 // Adjust based on pane height
				if m.clientScrollOffset < len(m.lockedDeviceInfo.AssociatedClients)-maxVisibleClients {
					m.clientScrollOffset++
				}
				return m, nil
			} else {
				// Normal target list navigation
				var cmd tea.Cmd
				m.targetList, cmd = m.targetList.Update(msg)
				return m, cmd
			}
		case "left", "h", "right", "l":
			// Always allow target list navigation
			var cmd tea.Cmd
			m.targetList, cmd = m.targetList.Update(msg)
			return m, cmd
		case "enter":
			if selectedItem, ok := m.targetList.SelectedItem().(*TargetItem); ok {
				displayValue := selectedItem.Value
				if selectedItem.TType == SSID {
					displayValue = selectedItem.OriginalValue
				}

				if selectedItem.IsIgnored() {
					selectedItem.ToggleIgnore()
					m.addTempMessage(fmt.Sprintf("Un-ignored: %s", displayValue))
				}

				// If we're switching from a locked target, auto-ignore it and unlock the channel
				if m.lockedTarget != nil && m.channelLocked {
					// Auto-ignore the current target
					m.lockedTarget.ToggleIgnore()
					currentDisplay := m.lockedTarget.Value
					if m.lockedTarget.TType == SSID && m.lockedTarget.OriginalValue != "" {
						currentDisplay = m.lockedTarget.OriginalValue
					}
					m.addTempMessage(fmt.Sprintf("Auto-ignored: %s", currentDisplay))
					
					// Update the target in the main targets list
					for _, target := range m.targets {
						if (m.lockedTarget.TType == MAC && target.Value == m.lockedTarget.Value) ||
							(m.lockedTarget.TType == SSID && target.OriginalValue == m.lockedTarget.OriginalValue) {
							target.Ignored = true
							break
						}
					}
					
					// Unlock the channel
					err := hopChannel(uuid, m.kismetEndpoint)
					if err != nil {
						log.Printf("Error unlocking previous channel: %v", err)
						m.addRealTimeOutput(fmt.Sprintf("Error unlocking previous channel: %v", err))
					}
				}

				// Reset all target-related state and let discovery find the new target
				m.lockedTarget = nil // Clear target to allow discovery logic to run
				m.lockedDeviceInfo = nil 
				m.channelLocked = false
				m.clientScrollOffset = 0
				m.focusOnClients = false
				m.rssi = MinRSSI
				m.channel = ""
				m.lastReceived = time.Now()

				m.addRealTimeOutput(fmt.Sprintf("Searching for target %s...", displayValue))
			}
			return m, nil
		case "i":
			if m.lockedTarget != nil {
				m.lockedTarget.ToggleIgnore()
				displayValue := m.lockedTarget.Value
				if m.lockedTarget.TType == SSID {
					displayValue = m.lockedTarget.OriginalValue
				}
				if m.lockedTarget.IsIgnored() {
					m.addTempMessage(fmt.Sprintf("Ignored: %s", displayValue))
				} else {
					m.addTempMessage(fmt.Sprintf("Un-ignored: %s", displayValue))
				}
				for _, target := range m.targets {
					if (m.lockedTarget.TType == MAC && target.Value == m.lockedTarget.Value) ||
						(m.lockedTarget.TType == SSID && target.OriginalValue == m.lockedTarget.OriginalValue) {
						target.Ignored = m.lockedTarget.Ignored
						break
					}
				}
				m.lockedTarget = nil
				m.lockedDeviceInfo = nil
				m.channel = ""
				m.clientScrollOffset = 0
				m.focusOnClients = false
				m.addRealTimeOutput("Searching for new target...")
				m.channelLocked = false
			}
			err := hopChannel(uuid, m.kismetEndpoint)
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
		m.targetList.SetWidth(m.windowWidth / 2)
		return m, nil

	case tickMsg:
		// Clear temporary messages after 3 seconds
		if time.Since(m.tempMsgTimer) > 3*time.Second {
			m.tempMessages = []string{}
		}
		
		devices, err := FetchAllDevices(m.kismetEndpoint)
		m.addKismetData(devices)
		if err == nil {
			m.addKismetData(devices)
		}

		if m.lockedTarget == nil {
			value, channel, targetItem, _ := FindValidTarget(m.targets, m.kismetEndpoint)
			if value != "" {
				m.lockedTarget = targetItem
				m.channel = channel
				m.channelLocked = false
			}
		}

		if m.lockedTarget != nil {
			// Fetch dynamic info periodically
			deviceInfo, err := FetchDeviceInfo(m.lockedTarget.Value, m.kismetEndpoint)
			if err != nil && err != errDeviceNotFound {
				log.Printf("Error fetching device info: %v", err)
			}
			if deviceInfo != nil {
				m.rssi = deviceInfo.RSSI
				m.channel = deviceInfo.Channel
				m.lastReceived = time.Now()
				m.lockedDeviceInfo = deviceInfo // Store device info for display

				// Lock the channel if not already locked
				if !m.channelLocked {
					if err := lockChannel(uuid, m.channel, m.kismetEndpoint); err != nil {
						m.addRealTimeOutput(fmt.Sprintf("Failed to lock channel: %v", err))
					} else {
						m.channelLocked = true
						m.addRealTimeOutput(fmt.Sprintf("Channel: %s", m.channel))
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
		if time.Since(m.lastReceived) > timeout && m.rssi > MinRSSI {
			m.rssi -= decayRate
			if m.rssi < MinRSSI {
				m.rssi = MinRSSI
			}
		}

		// Update progress bar
		percent := float64(m.rssi-MinRSSI) / float64(MaxRSSI-MinRSSI)
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

// Add new Kismet data to the model's buffer
func (m *Model) addKismetData(data []map[string]interface{}) {
	for _, device := range data {
		// Format device information (MAC, RSSI, etc.)
		mac, _ := device["kismet.device.base.macaddr"].(string)

		// rssi, _ := device["kismet.device.base.signal/kismet.common.signal.last_signal"].(float64)
		channel, _ := device["kismet.device.base.channel"].(string)
		// ssid, _ := device["dot11.device/dot11.device.last_beaconed_ssid_record/dot11.advertisedssid.ssid"].(string)

		// Create a formatted string to display
		entry := fmt.Sprintf("MAC: %s, Channel: %s", mac, channel)

		// Append to the data buffer
		m.kismetData = append(m.kismetData, entry)

		// Keep only the last `maxDataSize` entries
		if len(m.kismetData) > m.maxDataSize {
			m.kismetData = m.kismetData[len(m.kismetData)-m.maxDataSize:]
		}
	}
}

func (m *Model) View() string {
	topPaneWidth := m.windowWidth / 2

	topLeft := m.renderTargetListWithHelp(topPaneWidth)

	topRight := lipgloss.JoinVertical(
		lipgloss.Top,
		m.renderRSSIProgressBar(topPaneWidth),
		m.renderRSSIOverTimeChart(topPaneWidth),
	)

	var targetDisplay string
	if m.lockedTarget != nil {
		if m.lockedTarget.OriginalValue != "" && m.lockedTarget.TType == SSID {
			targetDisplay = m.lockedTarget.OriginalValue // Display SSID
		} else {
			targetDisplay = m.lockedTarget.Value // Display MAC address
		}
	}

	var bottomLeft string
	if m.lockedTarget == nil || !m.channelLocked {
		// Combine temp messages with real-time output when searching
		allMessages := append(m.tempMessages, m.realTimeOutput...)
		bottomLeft = renderRealTimePane("Searching for target(s)...", allMessages, topPaneWidth)
	} else {
		// When locked, show target info + temp messages
		var targetInfo []string
		if m.lockedDeviceInfo != nil {
			targetInfo = []string{
				fmt.Sprintf("Channel: %s", m.lockedDeviceInfo.Channel),
				fmt.Sprintf("Make: %s", m.lockedDeviceInfo.Manufacturer),
				fmt.Sprintf("SSID: %s", m.lockedDeviceInfo.SSID),
				fmt.Sprintf("Encryption: %s", m.lockedDeviceInfo.Crypt),
				fmt.Sprintf("Type: %s", m.lockedDeviceInfo.Type),
			}
		}
		// Add temp messages at the top, then target info
		allMessages := append(m.tempMessages, targetInfo...)
		bottomLeft = renderRealTimePane(fmt.Sprintf("Locked to target: %s", targetDisplay), allMessages, topPaneWidth)
	}

	bottomRight := m.renderLockedTargetPane(topPaneWidth)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, bottomRight)

	return lipgloss.JoinVertical(lipgloss.Top, topRow, bottomRow)
}

func (m *Model) renderRSSIOverTimeChart(width int) string {
	var builder strings.Builder

	minWidth := 31
	if width <= minWidth {
		return ""
	}

	maxRSSI, minRSSI := -30, -120
	height := 8

	// Adjust maxPoints to account for the left wall and make sure the dots don't disappear prematurely
	maxPoints := width - 30

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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4).
		Height(8).
		Render(builder.String())
}

func (m *Model) renderTargetListWithHelp(width int) string {
	listTitle := "Targets"

	var targetItems []list.Item
	for _, target := range m.targets {
		targetItems = append(targetItems, target)
	}

	m.targetList.SetItems(targetItems)

	macListView := m.targetList.View()
	m.targetList.SetShowHelp(false)
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
↑/k up • ↓/j down (navigate)
[Tab] Focus client list
[Enter] Search for targets
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
		Height(13).
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

func (m *Model) renderLockedTargetPane(width int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4).
		Height(13)

	var title string
	var content []string

	if m.lockedTarget == nil {
		title = "Target Information"
		content = []string{"No target locked"}
	} else if m.lockedDeviceInfo == nil {
		title = "Target Information"
		content = []string{"Fetching target details..."}
	} else {
		if m.focusOnClients {
			title = "Associated Clients [FOCUSED]"
		} else {
			title = "Associated Clients"
		}

		// Display basic target info (non-duplicate)
		targetDisplay := m.lockedTarget.Value
		if m.lockedTarget.TType == SSID && m.lockedTarget.OriginalValue != "" {
			targetDisplay = m.lockedTarget.OriginalValue
		}

		content = []string{
			fmt.Sprintf("Target: %s", targetDisplay),
			"",
		}

		// Display associated clients with sorting and scrolling
		if len(m.lockedDeviceInfo.AssociatedClients) > 0 {
			// Sort client MACs for consistent ordering
			var sortedClients []string
			for clientMac := range m.lockedDeviceInfo.AssociatedClients {
				sortedClients = append(sortedClients, clientMac)
			}
			sort.Strings(sortedClients)

			maxVisibleClients := 8 // Available lines in pane
			totalClients := len(sortedClients)

			// Calculate visible range based on scroll offset
			startIdx := m.clientScrollOffset
			endIdx := startIdx + maxVisibleClients
			if endIdx > totalClients {
				endIdx = totalClients
			}

			// Add scroll indicators
			if startIdx > 0 {
				content = append(content, "↑ Scroll up for more")
			}

			// Display visible clients
			for i := startIdx; i < endIdx; i++ {
				content = append(content, fmt.Sprintf("  %s", sortedClients[i]))
			}

			// Add bottom scroll indicator
			if endIdx < totalClients {
				content = append(content, fmt.Sprintf("↓ %d more clients", totalClients-endIdx))
			}

			// Add client count info
			if totalClients > maxVisibleClients {
				content = append(content, "", fmt.Sprintf("Total: %d clients", totalClients))
			}
		} else {
			content = append(content, "No associated clients")
		}

		// Add navigation hint when clients are present
		if len(m.lockedDeviceInfo.AssociatedClients) > 8 {
			if m.focusOnClients {
				content = append(content, "", "Use ↑/↓ to scroll")
			} else {
				content = append(content, "", "Press Tab to focus & scroll")
			}
		}
	}

	header := lipgloss.NewStyle().Bold(true).Render(title)
	body := lipgloss.NewStyle().Render(strings.Join(content, "\n"))

	return style.Render(header + "\n" + body)
}

func renderKismetPane(title string, data []string, width int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(width - 4)

	header := lipgloss.NewStyle().Bold(true).Render(title)
	body := lipgloss.NewStyle().Render(strings.Join(data, "\n"))

	return style.Render(header + "\n" + body)
}

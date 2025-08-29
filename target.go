package main

type TargetType int

const (
	MAC TargetType = iota + 1
	SSID
)

type TargetItem struct {
	Value string
	TType TargetType
	// This will store the 'value' when it is an SSID for display. The 'value' will now become a MAC
	OriginalValue string
	Ignored       bool
	Search        bool
	ChannelLocked bool
}

func (i TargetItem) Title() string {
	var title string
	if i.TType == MAC {
		title = "MAC: " + i.Value
	} else if i.TType == SSID && i.OriginalValue != "" {
		title = "SSID: " + i.OriginalValue
	} else {
		title = "SSID: " + i.Value
	}
	
	if i.Ignored {
		title += " [IGNORED]"
	}
	
	return title
}

func (i TargetItem) Description() string { return "" }
func (i TargetItem) FilterValue() string { return i.Value }

// Check if the TargetItem is currently being ignored
func (t *TargetItem) IsIgnored() bool {
	return t.Ignored
}

// Replace addToIgnoreList and removeFromIgnoreList with a single toggle function
func (t *TargetItem) ToggleIgnore() *TargetItem {
	t.Ignored = !t.Ignored
	return t
}

// // Enables search on the target Item
// func (t *TargetItem) EnableSearch() *TargetItem {
// 	t.Search = true
// 	return t
// }

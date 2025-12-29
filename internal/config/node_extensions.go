package config

// ManagementPanelType represents the type of management panel to install.
type ManagementPanelType string

const (
	ManagementPanelNone    ManagementPanelType = "none"
	ManagementPanelWebmin  ManagementPanelType = "webmin"
	ManagementPanel1Panel  ManagementPanelType = "1panel"
	ManagementPanelCockpit ManagementPanelType = "cockpit"
)

// ManagementPanelSupportedTypes lists all supported management panel types.
var ManagementPanelSupportedTypes = []ManagementPanelType{
	ManagementPanelWebmin,
	ManagementPanel1Panel,
	ManagementPanelCockpit,
}

// ManagementPanelConfig contains per-node management panel settings.
type ManagementPanelConfig struct {
	// Enabled enables management panel installation on this node.
	Enabled bool `json:"enabled"`
	// Type specifies which management panel to install: "webmin", "1panel", or "cockpit".
	// Default: "webmin"
	Type ManagementPanelType `json:"type,omitempty"`
}

// GetType returns the panel type, defaulting to Webmin if not specified.
func (m *ManagementPanelConfig) GetType() ManagementPanelType {
	if m.Type == "" || m.Type == ManagementPanelNone {
		return ManagementPanelWebmin // Default to Webmin
	}
	return m.Type
}

// GetSupportedTypes returns all supported management panel types.
func GetSupportedManagementPanelTypes() []ManagementPanelType {
	return ManagementPanelSupportedTypes
}

// FirewallConfig contains per-node firewall (iptables) settings.
type FirewallConfig struct {
	// ConfigurationEnabled controls whether firewall rules are processed for this node.
	// When false: skips applying any firewall rules (does NOT disable the OS firewall).
	// When true: applies the profiles and port rules defined below.
	ConfigurationEnabled bool `json:"configurationEnabled"`
	// Profiles are predefined firewall profiles to apply (in order).
	// Supported: "BlockAllPublic", "AllowAllPrivate", "Harden"
	Profiles []FirewallProfile `json:"profiles,omitempty"`
	// Ports are custom port rules to apply (applied after profiles).
	Ports []FirewallPortRule `json:"ports,omitempty"`
}

// FirewallPortRule represents a custom port rule.
type FirewallPortRule struct {
	// Enabled enables this port rule.
	Enabled bool `json:"enabled"`
	// Protocol is the protocol(s): ["TCP"], ["UDP"], or ["TCP", "UDP"].
	Protocol []string `json:"protocol"`
	// RangeList is a list of ports or port ranges: [80, 443, "4500-6500"].
	// Can be integers or strings for ranges.
	RangeList []interface{} `json:"rangeList"`
	// Sources is a list of source CIDRs or shortcuts.
	// Shortcuts: "private" (RFC1918), "cgnat" (RFC6598), "any" (0.0.0.0/0).
	// Examples: ["10.0.0.0/8", "192.168.1.0/24"] or ["private", "cgnat"].
	// Default: ["0.0.0.0/0"] (allow from anywhere).
	Sources []string `json:"sources,omitempty"`
	// Action is "ACCEPT" or "DROP" (default: "ACCEPT").
	Action string `json:"action,omitempty"`
	// Comment is an optional description for the rule.
	Comment string `json:"comment,omitempty"`
}

// GetAction returns the action, defaulting to ACCEPT if not specified.
func (r *FirewallPortRule) GetAction() string {
	if r.Action == "" {
		return "ACCEPT"
	}
	return r.Action
}

// GetSources returns the source CIDRs, defaulting to ["0.0.0.0/0"] if not specified.
func (r *FirewallPortRule) GetSources() []string {
	if len(r.Sources) == 0 {
		return []string{"0.0.0.0/0"}
	}
	return r.Sources
}

// HasFirewallEnabled checks if any firewall configuration is enabled.
func (f *FirewallConfig) HasFirewallEnabled() bool {
	if !f.ConfigurationEnabled {
		return false
	}
	// Check if any profiles are enabled
	for _, p := range f.Profiles {
		if p.Enabled {
			return true
		}
	}
	// Check if any port rules are enabled
	for _, r := range f.Ports {
		if r.Enabled {
			return true
		}
	}
	return false
}

// FirewallProfile represents a predefined firewall profile.
type FirewallProfile struct {
	// Enabled enables this profile.
	Enabled bool `json:"enabled"`
	// Name is the profile name: "BlockAllPublic", "AllowAllPrivate", or "Harden".
	Name string `json:"name"`
}

// Predefined firewall profile names.
const (
	// FirewallProfileBlockAllPublic blocks all inbound from public IPs,
	// only allowing established/related connections and loopback.
	FirewallProfileBlockAllPublic = "BlockAllPublic"
	// FirewallProfileAllowAllPrivate allows all traffic from RFC1918 + RFC6598 (CGNAT) ranges.
	FirewallProfileAllowAllPrivate = "AllowAllPrivate"
	// FirewallProfileHarden applies security hardening rules:
	// - Rate limiting on SSH (max 4 new connections per minute)
	// - SYN flood protection via tcp_syncookies
	// - ICMP rate limiting
	// - Drop invalid packets
	FirewallProfileHarden = "Harden"
)

// FirewallSupportedProfiles lists all supported firewall profile names.
var FirewallSupportedProfiles = []string{
	FirewallProfileBlockAllPublic,
	FirewallProfileAllowAllPrivate,
	FirewallProfileHarden,
}

// IsValidFirewallProfile checks if a profile name is valid.
func IsValidFirewallProfile(name string) bool {
	for _, p := range FirewallSupportedProfiles {
		if p == name {
			return true
		}
	}
	return false
}


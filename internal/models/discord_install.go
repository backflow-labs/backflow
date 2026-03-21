package models

import "time"

// DiscordInstall represents a Discord bot installation in a guild.
type DiscordInstall struct {
	GuildID      string    `json:"guild_id"`
	AppID        string    `json:"app_id"`
	ChannelID    string    `json:"channel_id"`
	AllowedRoles []string  `json:"allowed_roles,omitempty"`
	InstalledAt  time.Time `json:"installed_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

package discord

import "github.com/bwmarrin/discordgo"

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "backflow",
		Description: "Backflow agent orchestrator",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "run",
				Description: "Run an agent task",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "prompt",
						Description: "Task prompt",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
					{
						Name:        "repo",
						Description: "Repository URL (uses default if not set)",
						Type:        discordgo.ApplicationCommandOptionString,
					},
					{
						Name:        "branch",
						Description: "Working branch",
						Type:        discordgo.ApplicationCommandOptionString,
					},
					{
						Name:        "model",
						Description: "Claude model",
						Type:        discordgo.ApplicationCommandOptionString,
					},
					{
						Name:        "create_pr",
						Description: "Create a PR on completion",
						Type:        discordgo.ApplicationCommandOptionBoolean,
					},
					{
						Name:        "max_budget",
						Description: "Max budget in USD",
						Type:        discordgo.ApplicationCommandOptionNumber,
					},
				},
			},
		},
	},
}

func RegisterCommands(s *discordgo.Session, guildID string) ([]*discordgo.ApplicationCommand, error) {
	registered := make([]*discordgo.ApplicationCommand, 0, len(commands))
	for _, cmd := range commands {
		created, err := s.ApplicationCommandCreate(s.State.User.ID, guildID, cmd)
		if err != nil {
			return registered, err
		}
		registered = append(registered, created)
	}
	return registered, nil
}

func DeregisterCommands(s *discordgo.Session, guildID string, registered []*discordgo.ApplicationCommand) error {
	for _, cmd := range registered {
		if err := s.ApplicationCommandDelete(s.State.User.ID, guildID, cmd.ID); err != nil {
			return err
		}
	}
	return nil
}

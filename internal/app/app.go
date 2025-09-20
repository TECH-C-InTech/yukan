package app

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// Command represents a slash command the bot can register and handle.
type Command interface {
	Name() string
	Definition() *discordgo.ApplicationCommand
	Handle(session *discordgo.Session, interaction *discordgo.InteractionCreate)
}

// App manages the Discord session and command lifecycle.
type App struct {
	session       *discordgo.Session
	commands      map[string]Command
	registeredIDs map[string]string
	applicationID string
}

// New constructs a new App with the provided Discord bot token and commands.
func New(token string, commands ...Command) (*App, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

	app := &App{
		session:       session,
		commands:      make(map[string]Command, len(commands)),
		registeredIDs: make(map[string]string, len(commands)),
	}

	session.AddHandler(app.handleInteractionCreate)

	for _, cmd := range commands {
		if cmd == nil {
			return nil, fmt.Errorf("command is nil")
		}
		name := cmd.Name()
		if name == "" {
			return nil, fmt.Errorf("command name is empty")
		}
		if _, exists := app.commands[name]; exists {
			return nil, fmt.Errorf("duplicate command name detected: %s", name)
		}
		app.commands[name] = cmd
	}

	return app, nil
}

// Start opens the Discord session and registers all commands.
func (a *App) Start() error {
	if err := a.session.Open(); err != nil {
		return fmt.Errorf("failed to connect to Discord: %w", err)
	}

	log.Println("Discord session established")

	if a.session.State == nil || a.session.State.User == nil {
		return fmt.Errorf("discord session state is not populated")
	}
	a.applicationID = a.session.State.User.ID

	for name, cmd := range a.commands {
		definition := cmd.Definition()
		if definition == nil {
			return fmt.Errorf("command definition for %s is nil", name)
		}
		created, err := a.session.ApplicationCommandCreate(a.applicationID, "", definition)
		if err != nil {
			a.unregisterAll()
			_ = a.session.Close()
			return fmt.Errorf("failed to register /%s command: %w", name, err)
		}
		a.registeredIDs[name] = created.ID
		log.Printf("/%s command registered", name)
	}

	return nil
}

// Close unregisters commands and closes the Discord session.
func (a *App) Close() {
	a.unregisterAll()
	if err := a.session.Close(); err != nil {
		log.Printf("failed to close Discord session: %v", err)
	}
}

func (a *App) handleInteractionCreate(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := interaction.ApplicationCommandData()
	cmd, ok := a.commands[data.Name]
	if !ok {
		return
	}

	cmd.Handle(session, interaction)
}

// Session returns the underlying Discord session.
func (a *App) Session() *discordgo.Session {
	return a.session
}

func (a *App) unregisterAll() {
	if a.applicationID == "" {
		return
	}

	for name, id := range a.registeredIDs {
		if err := a.session.ApplicationCommandDelete(a.applicationID, "", id); err != nil {
			log.Printf("failed to delete command /%s: %v", name, err)
			continue
		}
		log.Printf("/%s command deleted", name)
		delete(a.registeredIDs, name)
	}
}

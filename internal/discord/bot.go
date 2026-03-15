package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// ThreadInfo tracks the mapping between a Discord thread and a Backflow task.
type ThreadInfo struct {
	ThreadID  string
	ChannelID string
	TaskID    string
	RepoURL   string
	Prompt    string
	Branch    string
}

// Bot manages the Discord bot lifecycle and slash command handling.
type Bot struct {
	session    *discordgo.Session
	store      store.Store
	config     *config.Config
	threads    *sync.Map // taskID -> *ThreadInfo
	waitingFor *sync.Map // threadID -> *ThreadInfo (awaiting user reply)
	notifier   *DiscordNotifier
	guildID    string
	registered []*discordgo.ApplicationCommand
}

// New creates a new Discord bot. Call Start() to connect.
func New(s store.Store, cfg *config.Config) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

	bot := &Bot{
		session:    session,
		store:      s,
		config:     cfg,
		threads:    &sync.Map{},
		waitingFor: &sync.Map{},
		guildID:    cfg.DiscordGuildID,
	}
	bot.notifier = &DiscordNotifier{bot: bot}

	session.AddHandler(bot.onInteractionCreate)
	session.AddHandler(bot.onMessageCreate)

	return bot, nil
}

// Notifier returns the notify.Notifier implementation backed by this bot.
func (b *Bot) Notifier() notify.Notifier {
	return b.notifier
}

// Start connects to Discord and registers slash commands.
func (b *Bot) Start(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord websocket: %w", err)
	}

	registered, err := RegisterCommands(b.session, b.guildID)
	if err != nil {
		log.Error().Err(err).Msg("failed to register discord commands")
		return fmt.Errorf("register commands: %w", err)
	}
	b.registered = registered

	b.restoreThreads(ctx)

	log.Info().Str("guild_id", b.guildID).Int("commands", len(registered)).Msg("discord bot started")
	return nil
}

// restoreThreads reloads thread mappings from the database for non-terminal tasks.
func (b *Bot) restoreThreads(ctx context.Context) {
	tasks, err := b.store.ListTasks(ctx, store.TaskFilter{})
	if err != nil {
		log.Error().Err(err).Msg("discord: failed to restore thread mappings")
		return
	}

	restored := 0
	for _, task := range tasks {
		if task.DiscordThreadID == "" || task.Status.IsTerminal() {
			continue
		}
		ti := &ThreadInfo{
			ThreadID:  task.DiscordThreadID,
			ChannelID: b.config.DiscordChannelID,
			TaskID:    task.ID,
			RepoURL:   task.RepoURL,
			Prompt:    task.Prompt,
			Branch:    task.Branch,
		}
		b.threads.Store(task.ID, ti)
		restored++
	}
	if restored > 0 {
		log.Info().Int("count", restored).Msg("discord: restored thread mappings from database")
	}
}

// Stop deregisters commands and closes the session.
func (b *Bot) Stop() {
	if len(b.registered) > 0 {
		if err := DeregisterCommands(b.session, b.guildID, b.registered); err != nil {
			log.Error().Err(err).Msg("failed to deregister discord commands")
		}
	}
	b.session.Close()
	log.Info().Msg("discord bot stopped")
}

// onInteractionCreate handles slash command interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != "backflow" {
		return
	}

	if len(data.Options) == 0 {
		return
	}

	switch data.Options[0].Name {
	case "run":
		b.handleRun(s, i, data.Options[0].Options)
	}
}

// handleRun processes the /backflow run slash command.
func (b *Bot) handleRun(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	// Acknowledge immediately (ephemeral)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Creating task...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})

	// Extract options
	optMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, o := range opts {
		optMap[o.Name] = o
	}

	prompt := optMap["prompt"].StringValue()

	repoURL := b.config.DiscordDefaultRepo
	if o, ok := optMap["repo"]; ok {
		repoURL = o.StringValue()
	}
	if repoURL == "" {
		b.editResponse(s, i, "Error: no repo URL provided and no default configured.")
		return
	}

	var branch string
	if o, ok := optMap["branch"]; ok {
		branch = o.StringValue()
	}

	model := b.config.DefaultModel
	if o, ok := optMap["model"]; ok {
		model = o.StringValue()
	}

	createPR := true
	if o, ok := optMap["create_pr"]; ok {
		createPR = o.BoolValue()
	}

	maxBudget := b.config.DefaultMaxBudget
	if o, ok := optMap["max_budget"]; ok {
		maxBudget = o.FloatValue()
	}

	// Create task
	now := time.Now().UTC()
	task := &models.Task{
		ID:            "bf_" + ulid.Make().String(),
		Status:        models.TaskStatusPending,
		RepoURL:       repoURL,
		Branch:        branch,
		Prompt:        prompt,
		Model:         model,
		MaxBudgetUSD:  maxBudget,
		MaxRuntimeMin: int(b.config.DefaultMaxRuntime.Minutes()),
		MaxTurns:      b.config.DefaultMaxTurns,
		CreatePR:      createPR,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := b.store.CreateTask(context.Background(), task); err != nil {
		log.Error().Err(err).Msg("discord: failed to create task")
		b.editResponse(s, i, "Failed to create task: "+err.Error())
		return
	}

	// Create thread in channel (ThreadStartComplex creates a thread without a parent message)
	threadName := truncate(prompt, 100)
	thread, err := s.ThreadStartComplex(b.config.DiscordChannelID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 1440, // 24 hours
		Type:                discordgo.ChannelTypeGuildPublicThread,
	})
	if err != nil {
		log.Error().Err(err).Str("task_id", task.ID).Msg("discord: failed to create thread")
		b.editResponse(s, i, fmt.Sprintf("Task `%s` created but failed to create thread: %s", task.ID, err.Error()))
		return
	}

	// Persist thread ID on the task
	task.DiscordThreadID = thread.ID
	if err := b.store.UpdateTask(context.Background(), task); err != nil {
		log.Error().Err(err).Str("task_id", task.ID).Msg("discord: failed to save thread ID")
	}

	ti := &ThreadInfo{
		ThreadID:  thread.ID,
		ChannelID: b.config.DiscordChannelID,
		TaskID:    task.ID,
		RepoURL:   repoURL,
		Prompt:    prompt,
		Branch:    branch,
	}
	b.threads.Store(task.ID, ti)

	// Post confirmation embed in thread
	_, _ = s.ChannelMessageSendEmbed(thread.ID, formatEvent(notify.Event{
		Type:      notify.EventTaskCreated,
		TaskID:    task.ID,
		RepoURL:   repoURL,
		Prompt:    prompt,
		Timestamp: now,
	}))

	b.editResponse(s, i, fmt.Sprintf("Task `%s` created! Follow along in <#%s>", task.ID, thread.ID))
	log.Info().Str("task_id", task.ID).Str("thread_id", thread.ID).Msg("discord: task created via slash command")
}

// onMessageCreate handles messages in tracked threads.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore bot's own messages
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Check if this message is in a tracked thread
	threadID := m.ChannelID
	ti := b.findThreadInfo(threadID)
	if ti == nil {
		return
	}

	content := strings.TrimSpace(m.Content)
	lower := strings.ToLower(content)

	// Handle thread commands
	switch lower {
	case "cancel":
		b.handleCancel(s, m, ti)
		return
	case "status":
		b.handleStatus(s, m, ti)
		return
	case "info":
		b.handleInfo(s, m, ti)
		return
	}

	// If waiting for Q&A answer, treat message as the answer
	if _, waiting := b.waitingFor.Load(threadID); waiting {
		b.handleAnswer(s, m, ti, content)
		return
	}
}

// handleCancel cancels or deletes the task.
func (b *Bot) handleCancel(s *discordgo.Session, m *discordgo.MessageCreate, ti *ThreadInfo) {
	task, err := b.store.GetTask(context.Background(), ti.TaskID)
	if err != nil || task == nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Could not find task.")
		return
	}

	if task.Status.IsTerminal() {
		_, _ = s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Task is already in terminal state: `%s`", task.Status))
		return
	}

	task.Status = models.TaskStatusCancelled
	now := time.Now().UTC()
	task.CompletedAt = &now
	if err := b.store.UpdateTask(context.Background(), task); err != nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Failed to cancel task: "+err.Error())
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title:       "Task Cancelled",
		Description: fmt.Sprintf("Task `%s` has been cancelled.", ti.TaskID),
		Color:       colorRed,
	})
}

// handleStatus posts the current task status.
func (b *Bot) handleStatus(s *discordgo.Session, m *discordgo.MessageCreate, ti *ThreadInfo) {
	task, err := b.store.GetTask(context.Background(), ti.TaskID)
	if err != nil || task == nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Could not find task.")
		return
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Status", Value: string(task.Status), Inline: true},
		{Name: "Model", Value: task.Model, Inline: true},
	}
	if task.CostUSD > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Cost", Value: fmt.Sprintf("$%.2f", task.CostUSD), Inline: true,
		})
	}
	if task.PRURL != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "PR", Value: task.PRURL,
		})
	}
	if task.Error != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Error", Value: truncate(task.Error, 1024),
		})
	}

	color := colorBlue
	switch task.Status {
	case models.TaskStatusRunning:
		color = colorYellow
	case models.TaskStatusCompleted:
		color = colorGreen
	case models.TaskStatusFailed, models.TaskStatusCancelled, models.TaskStatusInterrupted:
		color = colorRed
	}

	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title:  fmt.Sprintf("Task %s", task.ID),
		Color:  color,
		Fields: fields,
	})
}

// handleInfo posts task and container info.
func (b *Bot) handleInfo(s *discordgo.Session, m *discordgo.MessageCreate, ti *ThreadInfo) {
	task, err := b.store.GetTask(context.Background(), ti.TaskID)
	if err != nil || task == nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Could not find task.")
		return
	}

	if task.InstanceID == "" || task.ContainerID == "" {
		_, _ = s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No container assigned yet. Status: `%s`", task.Status))
		return
	}

	// LogFetcher is on the orchestrator — we can only show what we have
	// Post the task error/status as a fallback
	msg := fmt.Sprintf("**Status:** `%s`\n**Instance:** `%s`\n**Container:** `%s`", task.Status, task.InstanceID, task.ContainerID)
	if task.Error != "" {
		msg += fmt.Sprintf("\n**Error:** %s", truncate(task.Error, 1500))
	}
	_, _ = s.ChannelMessageSend(m.ChannelID, msg)
}

// handleAnswer processes a user's reply to an agent question.
func (b *Bot) handleAnswer(s *discordgo.Session, m *discordgo.MessageCreate, ti *ThreadInfo, answer string) {
	b.waitingFor.Delete(m.ChannelID)

	// Look up original task to inherit settings
	originalTask, err := b.store.GetTask(context.Background(), ti.TaskID)
	createPR := true
	if err == nil && originalTask != nil {
		createPR = originalTask.CreatePR
	}

	// Create a new task with the original prompt + context containing the Q&A
	taskContext := fmt.Sprintf("Previous agent asked:\n%s\n\nUser answered:\n%s", ti.Prompt, answer)

	now := time.Now().UTC()
	newTask := &models.Task{
		ID:              "bf_" + ulid.Make().String(),
		Status:          models.TaskStatusPending,
		RepoURL:         ti.RepoURL,
		Branch:          ti.Branch,
		Prompt:          ti.Prompt,
		Context:         taskContext,
		Model:           b.config.DefaultModel,
		MaxBudgetUSD:    b.config.DefaultMaxBudget,
		MaxRuntimeMin:   int(b.config.DefaultMaxRuntime.Minutes()),
		MaxTurns:        b.config.DefaultMaxTurns,
		CreatePR:        createPR,
		DiscordThreadID: ti.ThreadID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := b.store.CreateTask(context.Background(), newTask); err != nil {
		log.Error().Err(err).Msg("discord: failed to create follow-up task")
		_, _ = s.ChannelMessageSend(m.ChannelID, "Failed to create follow-up task: "+err.Error())
		return
	}

	// Track new task in same thread
	newTI := &ThreadInfo{
		ThreadID:  ti.ThreadID,
		ChannelID: ti.ChannelID,
		TaskID:    newTask.ID,
		RepoURL:   ti.RepoURL,
		Prompt:    ti.Prompt,
		Branch:    ti.Branch,
	}
	b.threads.Store(newTask.ID, newTI)

	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title:       "Follow-up Task Created",
		Description: fmt.Sprintf("Task `%s` queued with your answer.", newTask.ID),
		Color:       colorBlue,
	})

	log.Info().Str("task_id", newTask.ID).Str("parent_task", ti.TaskID).Msg("discord: follow-up task created from Q&A")
}

// findThreadInfo looks up ThreadInfo by thread ID (reverse lookup).
func (b *Bot) findThreadInfo(threadID string) *ThreadInfo {
	var found *ThreadInfo
	b.threads.Range(func(_, value any) bool {
		ti := value.(*ThreadInfo)
		if ti.ThreadID == threadID {
			found = ti
			return false // stop iteration
		}
		return true
	})
	return found
}

// editResponse edits the initial interaction response.
func (b *Bot) editResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
}

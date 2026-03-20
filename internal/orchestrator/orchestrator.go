package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// maxInspectFailures is the number of consecutive container inspect failures
// before a task is killed or requeued.
const maxInspectFailures = 3

// Orchestrator manages the lifecycle of tasks: dispatching them to instances,
// monitoring their containers, handling completions, and recovering from restarts.
type Orchestrator struct {
	store  store.Store
	config *config.Config
	bus    *notify.EventBus
	ec2    *EC2Manager
	docker dockerClient
	scaler scaler
	spot   *SpotHandler
	s3     s3Client

	mu              sync.Mutex
	running         int
	stopCh          chan struct{}
	inspectFailures map[string]int // task ID -> consecutive inspect failure count
}

func New(s store.Store, cfg *config.Config, bus *notify.EventBus, s3 s3Client) *Orchestrator {
	o := &Orchestrator{
		store:           s,
		config:          cfg,
		bus:             bus,
		stopCh:          make(chan struct{}),
		inspectFailures: make(map[string]int),
		s3:              s3,
	}

	switch cfg.Mode {
	case config.ModeLocal:
		o.docker = NewDockerManager(cfg)
		o.initLocalMode(s, cfg)
	case config.ModeFargate:
		o.docker = NewFargateManager(cfg, s3)
		o.initFargateMode(s, cfg)
	default:
		docker := NewDockerManager(cfg)
		o.docker = docker
		o.initEC2Mode(s, cfg, docker)
	}

	return o
}

// initLocalMode seeds a "local" instance so findAvailableInstance works without EC2.
func (o *Orchestrator) initLocalMode(s store.Store, cfg *config.Config) {
	o.scaler = localScaler{}
	o.syncSyntheticInstance(s, syntheticInstanceSpec{
		id:            "local",
		instanceType:  "local",
		maxContainers: cfg.ContainersPerInst,
		privateIP:     "127.0.0.1",
		getErrMsg:     "local init: failed to get synthetic instance",
	})
}

// initFargateMode seeds a synthetic "fargate" instance so the orchestrator can
// track capacity without managing VM lifecycle.
func (o *Orchestrator) initFargateMode(s store.Store, cfg *config.Config) {
	o.scaler = localScaler{}

	o.syncSyntheticInstance(s, syntheticInstanceSpec{
		id:            "fargate",
		instanceType:  "fargate",
		maxContainers: cfg.MaxConcurrentTasks,
		getErrMsg:     "fargate init: failed to get synthetic instance",
		createErrMsg:  "fargate init: failed to create synthetic instance",
	})
	o.terminateStaleInstances(s, "fargate")
}

// initEC2Mode sets up EC2 scaling, spot handling, and cleans up leftover local instances.
func (o *Orchestrator) initEC2Mode(s store.Store, cfg *config.Config, docker *DockerManager) {
	ec2 := NewEC2Manager(cfg)
	o.ec2 = ec2
	o.scaler = NewScaler(s, ec2, docker, cfg)
	o.spot = NewSpotHandler(s, ec2)

	// Clean up leftover local instance from a previous local-mode run.
	inst, err := s.GetInstance(context.Background(), "local")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Error().Err(err).Msg("ec2 init: failed to check for leftover local instance")
	} else if err == nil && inst.Status != models.InstanceStatusTerminated {
		s.UpdateInstanceStatus(context.Background(), "local", models.InstanceStatusTerminated)
	}
}

type syntheticInstanceSpec struct {
	id            string
	instanceType  string
	maxContainers int
	privateIP     string
	getErrMsg     string
	createErrMsg  string
}

// syncSyntheticInstance ensures a synthetic instance exists and is marked running.
// It is used by local and Fargate modes to keep capacity management consistent.
func (o *Orchestrator) syncSyntheticInstance(s store.Store, spec syntheticInstanceSpec) {
	ctx := context.Background()

	_, err := s.GetInstance(ctx, spec.id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		now := time.Now().UTC()
		inst := &models.Instance{
			InstanceID:    spec.id,
			InstanceType:  spec.instanceType,
			Status:        models.InstanceStatusRunning,
			MaxContainers: spec.maxContainers,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if spec.privateIP != "" {
			inst.PrivateIP = spec.privateIP
		}
		if err := s.CreateInstance(ctx, inst); err != nil && spec.createErrMsg != "" {
			log.Error().Err(err).Msg(spec.createErrMsg)
		}
	case err != nil:
		if spec.getErrMsg != "" {
			log.Error().Err(err).Msg(spec.getErrMsg)
		}
	default:
		s.UpdateInstanceStatus(ctx, spec.id, models.InstanceStatusRunning)
		s.ResetRunningContainers(ctx, spec.id)
	}
}

// terminateStaleInstances marks any non-synthetic instances as terminated.
func (o *Orchestrator) terminateStaleInstances(s store.Store, keepID string) {
	ctx := context.Background()

	instances, err := s.ListInstances(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("fargate init: failed to list instances for cleanup")
		return
	}
	for _, other := range instances {
		if other.InstanceID == keepID || other.Status == models.InstanceStatusTerminated {
			continue
		}
		if err := s.UpdateInstanceStatus(ctx, other.InstanceID, models.InstanceStatusTerminated); err != nil {
			log.Error().Err(err).Str("instance_id", other.InstanceID).Msg("fargate init: failed to terminate stale instance")
		}
	}
}

// Docker returns the DockerManager for use by the API logs endpoint.
func (o *Orchestrator) Docker() dockerClient {
	return o.docker
}

// Start begins the orchestrator poll loop, recovering orphaned tasks first.
func (o *Orchestrator) Start(ctx context.Context) {
	log.Info().
		Str("mode", string(o.config.Mode)).
		Str("auth_mode", string(o.config.AuthMode)).
		Int("max_concurrent", o.config.MaxConcurrent()).
		Dur("poll_interval", o.config.PollInterval).
		Msg("orchestrator started")

	o.recoverOnStartup(ctx)

	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("orchestrator stopping")
			return
		case <-o.stopCh:
			log.Info().Msg("orchestrator stopped")
			return
		case <-ticker.C:
			o.tick(ctx)
		}
	}
}

// Stop signals the orchestrator to exit its poll loop.
func (o *Orchestrator) Stop() {
	close(o.stopCh)
}

// tick runs a single orchestration cycle: monitor, dispatch, scale.
func (o *Orchestrator) tick(ctx context.Context) {
	o.monitorCancelled(ctx)
	o.monitorRecovering(ctx)
	o.monitorRunning(ctx)
	o.dispatchPending(ctx)
	o.scaler.Evaluate(ctx)
}

// --- Shared helpers used across dispatch, monitor, and recovery ---

// incrementRunning safely increments the running task counter.
func (o *Orchestrator) incrementRunning() {
	o.mu.Lock()
	o.running++
	o.mu.Unlock()
}

// decrementRunning safely decrements the running task counter.
func (o *Orchestrator) decrementRunning() {
	o.mu.Lock()
	if o.running > 0 {
		o.running--
	}
	o.mu.Unlock()
}

// releaseInstanceSlot decrements the running container count for an instance.
func (o *Orchestrator) releaseInstanceSlot(ctx context.Context, instanceID string) {
	if instanceID == "" {
		return
	}
	o.store.DecrementRunningContainers(ctx, instanceID)
}

// releaseSlot decrements both the running counter and the instance container count.
func (o *Orchestrator) releaseSlot(ctx context.Context, task *models.Task) {
	o.decrementRunning()
	o.releaseInstanceSlot(ctx, task.InstanceID)
}

// markInstanceTerminated sets an instance to terminated status if it isn't already.
func (o *Orchestrator) markInstanceTerminated(ctx context.Context, instanceID string) {
	if instanceID == "" {
		return
	}
	o.store.UpdateInstanceStatus(ctx, instanceID, models.InstanceStatusTerminated)
}

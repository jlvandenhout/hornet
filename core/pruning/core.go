package pruning

import (
	"context"

	"github.com/labstack/gommon/bytes"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hornet/pkg/daemon"
	"github.com/iotaledger/hornet/pkg/database"
	"github.com/iotaledger/hornet/pkg/model/milestone"
	"github.com/iotaledger/hornet/pkg/model/storage"
	"github.com/iotaledger/hornet/pkg/model/syncmanager"
	"github.com/iotaledger/hornet/pkg/pruning"
	"github.com/iotaledger/hornet/pkg/snapshot"
)

func init() {
	CoreComponent = &app.CoreComponent{
		Component: &app.Component{
			Name:           "Pruning",
			DepsFunc:       func(cDeps dependencies) { deps = cDeps },
			Params:         params,
			InitConfigPars: initConfigPars,
			Provide:        provide,
			Run:            run,
		},
	}
}

var (
	CoreComponent *app.CoreComponent
	deps          dependencies
)

type dependencies struct {
	dig.In
	SnapshotManager *snapshot.Manager
	PruningManager  *pruning.Manager
}

func initConfigPars(c *dig.Container) error {

	type cfgResult struct {
		dig.Out
		PruningPruneReceipts bool `name:"pruneReceipts"`
	}

	return c.Provide(func() cfgResult {
		return cfgResult{
			PruningPruneReceipts: ParamsPruning.PruneReceipts,
		}
	})
}

func provide(c *dig.Container) error {

	type pruningManagerDeps struct {
		dig.In
		Storage              *storage.Storage
		SyncManager          *syncmanager.SyncManager
		TangleDatabase       *database.Database `name:"tangleDatabase"`
		UTXODatabase         *database.Database `name:"utxoDatabase"`
		SnapshotManager      *snapshot.Manager
		PruningPruneReceipts bool `name:"pruneReceipts"`
	}

	return c.Provide(func(deps pruningManagerDeps) *pruning.Manager {

		pruningMilestonesEnabled := ParamsPruning.Milestones.Enabled
		pruningMilestonesMaxMilestonesToKeep := milestone.Index(ParamsPruning.Milestones.MaxMilestonesToKeep)

		if pruningMilestonesEnabled && pruningMilestonesMaxMilestonesToKeep == 0 {
			CoreComponent.LogPanicf("%s has to be specified if %s is enabled", CoreComponent.App.Config().GetParameterPath(&(ParamsPruning.Milestones.MaxMilestonesToKeep)), CoreComponent.App.Config().GetParameterPath(&(ParamsPruning.Milestones.Enabled)))
		}

		pruningSizeEnabled := ParamsPruning.Size.Enabled
		pruningTargetDatabaseSizeBytes, err := bytes.Parse(ParamsPruning.Size.TargetSize)
		if err != nil {
			CoreComponent.LogPanicf("parameter %s invalid", CoreComponent.App.Config().GetParameterPath(&(ParamsPruning.Size.TargetSize)))
		}

		if pruningSizeEnabled && pruningTargetDatabaseSizeBytes == 0 {
			CoreComponent.LogPanicf("%s has to be specified if %s is enabled", CoreComponent.App.Config().GetParameterPath(&(ParamsPruning.Size.TargetSize)), CoreComponent.App.Config().GetParameterPath(&(ParamsPruning.Size.Enabled)))
		}

		return pruning.NewPruningManager(
			CoreComponent.Logger(),
			deps.Storage,
			deps.SyncManager,
			deps.TangleDatabase,
			deps.UTXODatabase,
			deps.SnapshotManager.MinimumMilestoneIndex,
			pruningMilestonesEnabled,
			pruningMilestonesMaxMilestonesToKeep,
			pruningSizeEnabled,
			pruningTargetDatabaseSizeBytes,
			ParamsPruning.Size.ThresholdPercentage,
			ParamsPruning.Size.CooldownTime,
			deps.PruningPruneReceipts,
		)
	})
}

func run() error {

	newConfirmedMilestoneSignal := make(chan milestone.Index)
	onSnapshotHandledConfirmedMilestoneIndexChanged := events.NewClosure(func(msIndex milestone.Index) {
		select {
		case newConfirmedMilestoneSignal <- msIndex:
		default:
		}
	})

	if err := CoreComponent.Daemon().BackgroundWorker("Pruning", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting pruning background worker ... done")

		deps.SnapshotManager.Events.HandledConfirmedMilestoneIndexChanged.Attach(onSnapshotHandledConfirmedMilestoneIndexChanged)
		defer deps.SnapshotManager.Events.HandledConfirmedMilestoneIndexChanged.Detach(onSnapshotHandledConfirmedMilestoneIndexChanged)

		for {
			select {
			case <-ctx.Done():
				CoreComponent.LogInfo("Stopping pruning background worker...")
				CoreComponent.LogInfo("Stopping pruning background worker... done")
				return

			case confirmedMilestoneIndex := <-newConfirmedMilestoneSignal:
				deps.PruningManager.HandleNewConfirmedMilestoneEvent(ctx, confirmedMilestoneIndex)
			}
		}
	}, daemon.PriorityPruning); err != nil {
		CoreComponent.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}

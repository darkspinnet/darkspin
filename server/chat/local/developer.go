package local

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type itemSummoner struct {
	userManager *sporenet.UserManager
	gameManager *game.Manager
}

func NewItemSummoner(
	userManager *sporenet.UserManager,
	gameManager *game.Manager,
) chat.ItemSummoner {
	return itemSummoner{userManager: userManager, gameManager: gameManager}
}

func (a itemSummoner) SummonItem(ctx context.Context, command chat.ItemSummonCommand) error {
	if ctx == nil || a.userManager == nil || command.RigblockID == 0 {
		return chat.ErrItemSummonUnavailable
	}
	part := sporenet.NewPart(command.RigblockID)
	part.Level = 1
	part.Rarity = sporenet.PartBasic
	part.SetPrefix(command.PrimaryPrefix, false)
	part.SetPrefix(command.SecondaryPrefix, true)
	part.SetSuffix(command.Suffix)
	if part.RigblockAssetID != command.RigblockID ||
		part.PrefixAssetID != command.PrimaryPrefix ||
		part.PrefixSecondaryAssetID != command.SecondaryPrefix ||
		part.SuffixAssetID != command.Suffix {
		return chat.ErrItemSummonUnavailable
	}
	grantedPart, err := a.userManager.GrantPart(ctx, command.Sender.ID, part)
	if errors.Is(err, sporenet.ErrPartExists) {
		return chat.ErrItemExists
	}
	if err != nil {
		return fmt.Errorf("summonGrant: %w", err)
	}
	if command.GameID == 0 || a.gameManager == nil {
		return nil
	}
	instance := a.gameManager.Game(command.GameID)
	if instance != nil {
		instance.RequestItemPresentation(command.Sender.ID, grantedPart)
	}
	return nil
}

type levelSetter struct {
	userManager *sporenet.UserManager
	gameManager *game.Manager
}

func NewLevelSetter(
	userManager *sporenet.UserManager,
	gameManager *game.Manager,
) chat.LevelSetter {
	return levelSetter{userManager: userManager, gameManager: gameManager}
}

func (a levelSetter) SetLevel(ctx context.Context, command chat.LevelCommand) error {
	if ctx == nil || a.userManager == nil || command.Level == 0 || command.Level > 100 {
		return chat.ErrLevelUnavailable
	}
	err := a.userManager.SetAccountLevel(ctx, command.Sender.ID, command.Level)
	if err != nil {
		return fmt.Errorf("levelSet: %w", err)
	}
	user := a.userManager.UserByID(command.Sender.ID)
	if user == nil {
		return chat.ErrLevelUnavailable
	}
	view := user.View()
	if command.GameID == 0 || a.gameManager == nil {
		return nil
	}
	instance := a.gameManager.Game(command.GameID)
	if instance != nil {
		instance.RequestPlayerLevelUpdate(command.Sender.ID, game.PlayerLevelUpdate{
			Level: view.Account.Level,
			XP:    float32(view.Account.XP),
		})
	}
	return nil
}

type warpRequester struct {
	gameManager *game.Manager
}

func NewWarpRequester(gameManager *game.Manager) chat.WarpRequester {
	return warpRequester{gameManager: gameManager}
}

func (e warpRequester) RequestWarp(ctx context.Context, req chat.WarpCommand) error {
	if ctx == nil || e.gameManager == nil {
		return chat.ErrWarpUnavailable
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("warpContext: %w", err)
	}
	if !e.gameManager.RequestCampaignWarp(req.Sender.ID, req.Level) {
		return chat.ErrWarpUnavailable
	}
	return nil
}

type npcSpawner struct {
	gameManager *game.Manager
}

func NewNPCSpawner(gameManager *game.Manager) chat.NPCSpawner {
	return npcSpawner{gameManager: gameManager}
}

func (e npcSpawner) RequestNPCSpawn(
	ctx context.Context, req chat.NPCSpawnCommand,
) error {
	if ctx == nil || e.gameManager == nil || req.GameID == 0 || req.NounName == "" {
		return chat.ErrNPCSpawnUnavailable
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("npcSpawnContext: %w", err)
	}
	instance := e.gameManager.Game(req.GameID)
	if instance == nil || !instance.IsWarped() ||
		!instance.RequestPlayerEventCommand(req.Sender.ID, game.PlayerEventCommand{
			Name: "spawn", NounName: req.NounName,
		}) {
		return chat.ErrNPCSpawnUnavailable
	}
	return nil
}

type dnaGranter struct {
	userManager *sporenet.UserManager
	gameManager *game.Manager
}

func NewDNAGranter(
	userManager *sporenet.UserManager,
	gameManager *game.Manager,
) chat.DNAGranter {
	return dnaGranter{userManager: userManager, gameManager: gameManager}
}

func (a dnaGranter) GrantDNA(ctx context.Context, command chat.DNACommand) (uint32, error) {
	if ctx == nil || a.userManager == nil || command.Amount == 0 {
		return 0, chat.ErrDNAUnavailable
	}
	dna, err := a.userManager.GrantDNA(ctx, command.Sender.ID, command.Amount)
	if errors.Is(err, sporenet.ErrDNAOverflow) {
		return 0, chat.ErrDNAOverflow
	}
	if err != nil {
		return 0, fmt.Errorf("dnaGrant: %w", err)
	}
	if command.GameID == 0 || a.gameManager == nil {
		return dna, nil
	}
	instance := a.gameManager.Game(command.GameID)
	if instance != nil {
		instance.RequestPlayerDNAUpdate(command.Sender.ID, game.PlayerDNAUpdate{DNA: dna})
	}
	return dna, nil
}

type resourceMutator struct {
	gameManager *game.Manager
}

func NewResourceMutator(gameManager *game.Manager) chat.ResourceMutator {
	return resourceMutator{gameManager: gameManager}
}

func (a resourceMutator) RequestResourceMutation(
	_ context.Context,
	command chat.ResourceCommand,
) error {
	if a.gameManager == nil || command.GameID == 0 {
		return chat.ErrResourceUnavailable
	}
	instance := a.gameManager.Game(command.GameID)
	if instance == nil || !instance.RequestPlayerResourceCommand(command.Sender.ID, game.PlayerResourceCommand{
		Damage: command.Damage, PowerReduction: command.PowerReduction,
		IsHeal: command.IsHeal, IsPowerFill: command.IsPowerFill,
	}) {
		return chat.ErrResourceUnavailable
	}
	return nil
}

type eventTriggerer struct {
	gameManager *game.Manager
}

func NewEventTriggerer(gameManager *game.Manager) chat.EventTriggerer {
	return eventTriggerer{gameManager: gameManager}
}

func (a eventTriggerer) RequestEvent(_ context.Context, command chat.EventCommand) error {
	if a.gameManager == nil || command.GameID == 0 || command.Name == "" {
		return chat.ErrEventUnavailable
	}
	instance := a.gameManager.Game(command.GameID)
	if instance == nil || !instance.RequestPlayerEventCommand(command.Sender.ID, game.PlayerEventCommand{
		Name: command.Name, Category: command.Category,
		Position: game.Vec3{
			X: command.X,
			Y: command.Y,
			Z: command.Z,
		},
	}) {
		return chat.ErrEventUnavailable
	}
	return nil
}

type followRequester struct {
	gameManager *game.Manager
}

func NewFollowRequester(gameManager *game.Manager) chat.FollowRequester {
	return followRequester{gameManager: gameManager}
}

func (a followRequester) RequestFollow(
	_ context.Context, command chat.FollowCommand, target chat.Participant,
) error {
	if a.gameManager == nil || command.GameID == 0 || target.ID <= 0 {
		return chat.ErrFollowUnavailable
	}
	instance := a.gameManager.Game(command.GameID)
	if instance == nil || !instance.RequestPlayerEventCommand(command.Sender.ID, game.PlayerEventCommand{
		Name: "follow", TargetUserID: uint64(target.ID),
	}) {
		return chat.ErrFollowUnavailable
	}
	return nil
}

type effectPreviewer struct {
	gameManager *game.Manager
}

func NewEffectPreviewer(gameManager *game.Manager) chat.EffectPreviewer {
	return effectPreviewer{gameManager: gameManager}
}

func (a effectPreviewer) RequestEffectPreview(
	ctx context.Context,
	command chat.EffectPreviewCommand,
) error {
	if ctx == nil || a.gameManager == nil || command.GameID == 0 || command.Asset == 0 {
		return chat.ErrEffectPreviewUnavailable
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("effectContext: %w", err)
	}
	instance := a.gameManager.Game(command.GameID)
	preview := game.EffectPreview{Asset: command.Asset}
	if instance == nil || !instance.RequestEffectPreview(command.Sender.ID, preview) {
		return chat.ErrEffectPreviewUnavailable
	}
	return nil
}

var _ chat.ItemSummoner = itemSummoner{}
var _ chat.LevelSetter = levelSetter{}
var _ chat.WarpRequester = warpRequester{}
var _ chat.NPCSpawner = npcSpawner{}
var _ chat.DNAGranter = dnaGranter{}
var _ chat.ResourceMutator = resourceMutator{}
var _ chat.EventTriggerer = eventTriggerer{}
var _ chat.EffectPreviewer = effectPreviewer{}

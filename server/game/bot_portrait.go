package game

// BotPortraitFinder supplies the public portrait selected for a live bot.
type BotPortraitFinder interface {
	BotAvatar(uint64) (uint32, bool)
}

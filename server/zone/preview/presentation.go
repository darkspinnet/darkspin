package preview

import "github.com/darkspinnet/darkspin/server/util"

type Presentation struct {
	CurrentMovie uint32
	CurrentVoice uint32
	NextMovie    uint32
}

type campaignScene struct {
	chainLevelIndex uint32
	movieName       string
	voiceName       string
}

var campaignScenes = []campaignScene{
	{chainLevelIndex: 1, movieName: "cam_fmv_02_zelems"},
	{chainLevelIndex: 3, movieName: "cam_fmv_03_nocturna"},
	{chainLevelIndex: 5, movieName: "cam_fmv_04_verdanth"},
	{chainLevelIndex: 9, movieName: "cam_fmv_05_cryos"},
	{chainLevelIndex: 13, movieName: "cam_fmv_06_infinity"},
	{chainLevelIndex: 21, movieName: "cam_fmv_07_scaldron"},
}

const campaignEpilogueMovieName = "cam_fmv_08_epilogue"

// CampaignPresentation selects only story scenes authored for the current or
// immediately following first-pass chain slot. Later difficulty passes reuse
// maps without replaying the first-pass planet introductions. The epilogue is
// offered only after the first-pass finale has completed.
func CampaignPresentation(
	chainLevelIndex uint32, isCompleted bool,
) Presentation {
	presentation := Presentation{}
	if chainLevelIndex == 0 || chainLevelIndex > 24 {
		return presentation
	}
	for _, scene := range campaignScenes {
		if scene.chainLevelIndex == chainLevelIndex {
			presentation.CurrentMovie = util.HashID(scene.movieName)
			if scene.voiceName != "" {
				presentation.CurrentVoice = util.HashID(scene.voiceName)
			}
		}
		if scene.chainLevelIndex == chainLevelIndex+1 {
			presentation.NextMovie = util.HashID(scene.movieName)
		}
	}
	if isCompleted && chainLevelIndex == 24 {
		presentation.NextMovie = util.HashID(campaignEpilogueMovieName)
	}
	return presentation
}

// CampaignEntryPresentation selects only a movie authored on the level being
// entered for the first time. The result transition owns any following scene;
// entry must not pull that scene backward or invent a cue for a silent level.
func CampaignEntryPresentation(
	chainLevelIndex uint32, chainProgression uint32,
) Presentation {
	if chainLevelIndex == 0 || chainProgression >= chainLevelIndex {
		return Presentation{}
	}
	presentation := CampaignPresentation(chainLevelIndex, false)
	presentation.NextMovie = 0
	return presentation
}

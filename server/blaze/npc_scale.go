package blaze

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

const npcScaleSyntax = "Syntax: /scale <0.25-4|reset> [radius 1-100, default 30] [noun filter]; visual preview only"

func previewNPCScaleCommand(ctx context.Context, messenger *chat.Service, user *sporenet.User, body string) (string, error) {
	fields := strings.Fields(body)
	if len(fields) < 2 || len(fields) > 4 {
		return npcScaleSyntax, nil
	}
	req := chat.NPCScaleRequest{
		Sender: chat.Participant{ID: user.Account.ID, Name: user.DisplayName},
		GameID: user.CurrentGameID(), Radius: 30, Multiplier: 1,
		IsReset: strings.EqualFold(fields[1], "reset"),
	}
	if !req.IsReset {
		multiplier, err := strconv.ParseFloat(fields[1], 32)
		if err != nil || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0.25 || multiplier > 4 {
			return npcScaleSyntax, nil
		}
		req.Multiplier = float32(multiplier)
	}
	if len(fields) >= 3 {
		radius, err := strconv.ParseFloat(fields[2], 32)
		if err != nil {
			if len(fields) != 3 {
				return npcScaleSyntax, nil
			}
			req.NounFilter = fields[2]
		} else if math.IsNaN(radius) || math.IsInf(radius, 0) || radius < 1 || radius > 100 {
			return npcScaleSyntax, nil
		} else {
			req.Radius = float32(radius)
		}
	}
	if len(fields) == 4 {
		req.NounFilter = fields[3]
	}
	previews, err := messenger.PreviewNPCScale(ctx, req)
	if errors.Is(err, chat.ErrNPCScaleUnavailable) {
		return "Scale preview requires an active mission", nil
	}
	if err != nil {
		return "", fmt.Errorf("scaleCommand: %w", err)
	}
	if len(previews) == 0 {
		return "No matching live enemy NPCs within the selected radius", nil
	}
	nounCounts := make(map[string]int)
	for _, preview := range previews {
		nounCounts[preview.NounName]++
	}
	nounNames := make([]string, 0, len(nounCounts))
	for nounName := range nounCounts {
		nounNames = append(nounNames, nounName)
	}
	sort.Strings(nounNames)
	descriptions := make([]string, 0, len(nounNames))
	for _, nounName := range nounNames {
		if len(descriptions) == 8 {
			descriptions = append(descriptions, "more nouns listed in /bug metadata")
			break
		}
		descriptions = append(descriptions, fmt.Sprintf("%s (%d)", nounName, nounCounts[nounName]))
	}
	verb := fmt.Sprintf("Preview %.2fx", req.Multiplier)
	if req.IsReset {
		verb = "Reset preview"
	}
	return fmt.Sprintf("%s: %d NPCs within %.0f units; %s", verb, len(previews), req.Radius, strings.Join(descriptions, ", ")), nil
}

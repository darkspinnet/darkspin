package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// writeNPCDeathAnimations follows each enemy noun's own animation rig instead
// of borrowing the tutorial spider's death for uncurated campaign enemies.
func writeNPCDeathAnimations(ctx context.Context, transaction *sql.Tx, installPath string) error {
	rows, err := transaction.QueryContext(ctx, "SELECT DISTINCT noun_name FROM non_player_class ORDER BY noun_name")
	if err != nil {
		return fmt.Errorf("deathNouns: %w", err)
	}
	nouns := make(map[uint32]string)
	for rows.Next() {
		var nounName string
		err = rows.Scan(&nounName)
		if err != nil {
			closeErr := rows.Close()
			return fmt.Errorf("deathNounScan: %w", errors.Join(err, closeErr))
		}
		nouns[hashID(strings.TrimSuffix(nounName, ".Noun"))] = nounName
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil || closeErr != nil {
		return fmt.Errorf("deathNounRows: %w", errors.Join(rowErr, closeErr))
	}
	r, err := os.Open(filepath.Join(installPath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return fmt.Errorf("deathPackageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("deathPackageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("deathPackageRead: %w", err)
	}
	animations := make(map[uint32]string)
	for index, entry := range pkg.Entries {
		if entry.Type != characterAnimationAssetType || entry.Group != nounAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("deathAnimationOpen[%d]: %w", index, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("deathAnimationRead[%d]: %w", index, readErr)
		}
		for _, field := range bytes.Split(payload, []byte{0}) {
			animationName := string(field)
			if !strings.Contains(animationName, "_death") ||
				strings.Contains(animationName, "_crit") ||
				strings.Contains(animationName, "knockback") {
				continue
			}
			// Only select literal state names carried by this rig's resource.
			if strings.IndexFunc(animationName, isInvalidDeathStateRune) >= 0 {
				continue
			}
			animations[uint32(entry.Instance)] = animationName
			break
		}
	}
	for index, entry := range pkg.Entries {
		nounName, isNPC := nouns[uint32(entry.Instance)]
		if !isNPC || entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("deathNounOpen[%d]: %w", index, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("deathNounRead[%d]: %w", index, readErr)
		}
		rigName, isRigFound := readStringWithSuffix(payload, nounFixedSize, 8, ".CharacterAnimation")
		if !isRigFound {
			continue
		}
		animationName := animations[hashID(strings.TrimSuffix(rigName, ".CharacterAnimation"))]
		if animationName == "" {
			continue
		}
		result, insertErr := transaction.ExecContext(ctx,
			"INSERT INTO npc_death_animation (noun_name, animation_name) VALUES (?, ?)",
			nounName, animationName)
		if insertErr != nil {
			return fmt.Errorf("deathInsert[%d]: %w", index, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("deathInsertCount[%d]: %w", index, countErr)
		}
		if count != 1 {
			return fmt.Errorf("deathInsertCount[%d]: got %d", index, count)
		}
	}
	return nil
}

func isInvalidDeathStateRune(character rune) bool {
	return character != '_' && (character < 'a' || character > 'z') &&
		(character < 'A' || character > 'Z') && (character < '0' || character > '9')
}

func (e *Store) NPCDeathAnimations(ctx context.Context) (map[string]string, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("death catalog unavailable")
	}
	rows, err := e.database.QueryContext(ctx,
		"SELECT noun_name, animation_name FROM npc_death_animation ORDER BY noun_name")
	if err != nil {
		return nil, fmt.Errorf("deathQuery: %w", err)
	}
	defer rows.Close()
	animations := make(map[string]string)
	for rows.Next() {
		var nounName, animationName string
		err = rows.Scan(&nounName, &animationName)
		if err != nil {
			return nil, fmt.Errorf("deathScan: %w", err)
		}
		animations[strings.ToLower(nounName)] = animationName
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("deathRows: %w", err)
	}
	return animations, nil
}

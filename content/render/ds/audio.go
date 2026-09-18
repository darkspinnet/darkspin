package ds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
)

type audioManifestResource struct {
	ordinal  int
	resource dbpf.Resource
}

func projectAudioResources(ctx context.Context, pkg *dbpf.Reader, destinationPath string, manifest *dbpf.Manifest) error {
	snrResources := make(map[uint64]audioManifestResource)
	snsResources := make(map[uint64]audioManifestResource)
	for ordinal, resource := range manifest.Resources {
		switch resource.Entry.Type {
		case audio.SNRResourceType:
			snrResources[resource.Entry.Instance] = audioManifestResource{ordinal: ordinal, resource: resource}
		case audio.SNSResourceType:
			snsResources[resource.Entry.Instance] = audioManifestResource{ordinal: ordinal, resource: resource}
		}
	}
	if len(snrResources) == 0 {
		return nil
	}
	decoderPath, err := audioDecoderPath()
	if err != nil {
		return fmt.Errorf("audioDecoder: %w", err)
	}
	temporaryPath, err := os.MkdirTemp(destinationPath, ".audio-decode-*")
	if err != nil {
		return fmt.Errorf("audioTemporary: %w", err)
	}
	defer os.RemoveAll(temporaryPath)
	for instanceID, snrRecord := range snrResources {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("audioContext: %w", err)
		}
		snrDefinitionPath, pathErr := safePath(destinationPath, snrRecord.resource.PayloadPath)
		if pathErr != nil {
			return fmt.Errorf("snrPath: %w", pathErr)
		}
		identity := resourceDefinitionIdentity(snrRecord.resource.PayloadPath)
		temporaryName := fmt.Sprintf("%016x", instanceID)
		temporarySNRPath := filepath.Join(temporaryPath, temporaryName+".snr")
		err = writeDecodedAudioPath(pkg, snrRecord.ordinal, temporarySNRPath)
		if err != nil {
			return fmt.Errorf("snrWrite: %w", err)
		}
		snsRecord, isSNSFound := snsResources[instanceID]
		if isSNSFound {
			temporarySNSPath := filepath.Join(temporaryPath, temporaryName+".sns")
			err = writeDecodedAudioPath(pkg, snsRecord.ordinal, temporarySNSPath)
			if err != nil {
				return fmt.Errorf("snsWrite: %w", err)
			}
		}
		wavPath := filepath.Join(filepath.Dir(snrDefinitionPath), identity+".wav")
		command := exec.CommandContext(ctx, decoderPath, "-i", "-o", wavPath, temporarySNRPath)
		output, decodeErr := command.CombinedOutput()
		if decodeErr != nil {
			return fmt.Errorf("audioDecode[%016X]: %w: %s", instanceID, decodeErr, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func copyAudioWAVs(sourcePath, destinationPath string, manifest *dbpf.Manifest) error {
	for ordinal, resource := range manifest.Resources {
		if resource.Entry.Type != audio.SNRResourceType {
			continue
		}
		sourceDefinitionPath, err := safePath(sourcePath, resource.PayloadPath)
		if err != nil {
			return fmt.Errorf("sourceDefinition[%d]: %w", ordinal, err)
		}
		destinationDefinitionPath, err := safePath(destinationPath, resource.PayloadPath)
		if err != nil {
			return fmt.Errorf("destinationDefinition[%d]: %w", ordinal, err)
		}
		sourceWAVPath := strings.TrimSuffix(sourceDefinitionPath, filepath.Ext(sourceDefinitionPath)) + ".wav"
		destinationWAVPath := strings.TrimSuffix(destinationDefinitionPath, filepath.Ext(destinationDefinitionPath)) + ".wav"
		r, err := os.Open(sourceWAVPath)
		if err != nil {
			return fmt.Errorf("waveOpen[%d]: %w", ordinal, err)
		}
		w, err := os.OpenFile(destinationWAVPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			r.Close()
			return fmt.Errorf("waveCreate[%d]: %w", ordinal, err)
		}
		_, copyErr := io.Copy(w, r)
		closeWriteErr := w.Close()
		closeReadErr := r.Close()
		if copyErr != nil {
			return fmt.Errorf("waveCopy[%d]: %w", ordinal, copyErr)
		}
		if closeWriteErr != nil {
			return fmt.Errorf("waveClose[%d]: %w", ordinal, closeWriteErr)
		}
		if closeReadErr != nil {
			return fmt.Errorf("waveSourceClose[%d]: %w", ordinal, closeReadErr)
		}
	}
	return nil
}

func projectPackageAudioResource(ctx context.Context, pkg *dbpf.Reader, ordinal int, definitionPath string) error {
	entry := pkg.Entries[ordinal]
	if !audio.IsStreamType(entry.Type) {
		return nil
	}
	snrOrdinal := ordinal
	snsOrdinal := -1
	if entry.Type == audio.SNSResourceType {
		snsOrdinal = ordinal
		snrOrdinal = -1
	}
	for candidateOrdinal, candidate := range pkg.Entries {
		if candidate.Instance != entry.Instance {
			continue
		}
		if candidate.Type == audio.SNRResourceType {
			snrOrdinal = candidateOrdinal
		}
		if candidate.Type == audio.SNSResourceType {
			snsOrdinal = candidateOrdinal
		}
	}
	if snrOrdinal < 0 {
		return fmt.Errorf("snrMissing: instance 0x%016X", entry.Instance)
	}
	decoderPath, err := audioDecoderPath()
	if err != nil {
		return fmt.Errorf("audioDecoder: %w", err)
	}
	temporaryPath, err := os.MkdirTemp(filepath.Dir(definitionPath), ".audio-decode-*")
	if err != nil {
		return fmt.Errorf("audioTemporary: %w", err)
	}
	defer os.RemoveAll(temporaryPath)
	temporarySNRPath := filepath.Join(temporaryPath, "selected.snr")
	err = writeDecodedAudioPath(pkg, snrOrdinal, temporarySNRPath)
	if err != nil {
		return fmt.Errorf("snrWrite: %w", err)
	}
	if snsOrdinal >= 0 {
		err = writeDecodedAudioPath(pkg, snsOrdinal, filepath.Join(temporaryPath, "selected.sns"))
		if err != nil {
			return fmt.Errorf("snsWrite: %w", err)
		}
	}
	wavPath := strings.TrimSuffix(definitionPath, filepath.Ext(definitionPath)) + ".wav"
	command := exec.CommandContext(ctx, decoderPath, "-i", "-o", wavPath, temporarySNRPath)
	output, decodeErr := command.CombinedOutput()
	if decodeErr != nil {
		return fmt.Errorf("audioDecode: %w: %s", decodeErr, strings.TrimSpace(string(output)))
	}
	err = replaceAudioWAV(definitionPath, filepath.Base(wavPath))
	if err != nil {
		return fmt.Errorf("waveReference: %w", err)
	}
	return nil
}

func replaceAudioWAV(definitionPath, relativeWAVPath string) error {
	payload, err := os.ReadFile(definitionPath)
	if err != nil {
		return fmt.Errorf("definitionRead: %w", err)
	}
	lines := strings.Split(string(payload), "\n")
	isFound := false
	for lineIndex, line := range lines {
		if !strings.HasPrefix(line, "\tWAV ") {
			continue
		}
		if isFound {
			return errors.New("waveDuplicate")
		}
		lines[lineIndex] = fmt.Sprintf("\tWAV %q", relativeWAVPath)
		isFound = true
	}
	if !isFound {
		return errors.New("waveMissing")
	}
	err = os.WriteFile(definitionPath, []byte(strings.Join(lines, "\n")), 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func writeDecodedAudioPath(pkg *dbpf.Reader, ordinal int, destinationPath string) error {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return fmt.Errorf("ordinalRange: %d", ordinal)
	}
	payloadReader, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return fmt.Errorf("resourceOpen: %w", err)
	}
	w, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("destinationCreate: %w", err)
	}
	_, err = io.Copy(w, payloadReader)
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("resourceCopy: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("destinationClose: %w", closeErr)
	}
	return nil
}

func audioDSRoot(definitionPath string) (string, error) {
	directoryPath := filepath.Dir(definitionPath)
	for {
		manifestPath := filepath.Join(directoryPath, ManifestName)
		r, err := os.Open(manifestPath)
		if err == nil {
			parser := newParser(r)
			definition, readErr := parser.next()
			closeErr := r.Close()
			if readErr != nil {
				return "", fmt.Errorf("manifestRead: %w", readErr)
			}
			if closeErr != nil {
				return "", fmt.Errorf("manifestClose: %w", closeErr)
			}
			if len(definition) > 0 && (definition[0] == "PACKAGE" || definition[0] == "AUDIO" || definition[0] == "DARKSPINPACKAGE" || definition[0] == "DARKSPINAUDIO") {
				return directoryPath, nil
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("manifestStat: %w", err)
		}
		parentPath := filepath.Dir(directoryPath)
		if parentPath == directoryPath {
			return filepath.Dir(definitionPath), nil
		}
		directoryPath = parentPath
	}
}

func safeAudioPath(dsRoot, definitionDirectory, relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("absolutePath: %q", relativePath)
	}
	candidatePath, err := filepath.Abs(filepath.Join(definitionDirectory, filepath.FromSlash(relativePath)))
	if err != nil {
		return "", fmt.Errorf("pathAbsolute: %w", err)
	}
	rootPath, err := filepath.Abs(dsRoot)
	if err != nil {
		return "", fmt.Errorf("rootAbsolute: %w", err)
	}
	relativeCandidate, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return "", fmt.Errorf("pathRelative: %w", err)
	}
	if relativeCandidate == ".." || strings.HasPrefix(relativeCandidate, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pathEscape: %q", relativePath)
	}
	return candidatePath, nil
}

func audioDecoderPath() (string, error) {
	decoderName := "vgmstream-cli"
	if runtime.GOOS == "windows" {
		decoderName += ".exe"
	}
	decoderPath, err := exec.LookPath(decoderName)
	if err == nil {
		return decoderPath, nil
	}
	executablePath, executableErr := os.Executable()
	if executableErr == nil {
		candidatePath := filepath.Join(filepath.Dir(executablePath), decoderName)
		if _, statErr := os.Stat(candidatePath); statErr == nil {
			return candidatePath, nil
		}
	}
	workingPath, workingErr := os.Getwd()
	if workingErr == nil {
		candidatePath := filepath.Join(workingPath, ".cache", "vgmstream", decoderName)
		if _, statErr := os.Stat(candidatePath); statErr == nil {
			return candidatePath, nil
		}
	}
	return "", errors.New("audioDecoderMissing: download vgmstream-cli from " +
		"https://github.com/vgmstream/vgmstream/releases " +
		"and install it in PATH or beside darkrun")
}

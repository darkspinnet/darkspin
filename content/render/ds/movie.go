package ds

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/movie"
)

func writeMovieResource(destinationPath, identity string, payload []byte) error {
	document, err := movie.Decode(payload)
	if err != nil {
		return fmt.Errorf("containerDecode: %w", err)
	}
	for index, chunk := range document.Chunks {
		if chunk.Tag == "MVhd" || chunk.Tag == "MV0K" || chunk.Tag == "MV0F" || chunk.Tag == "SCHl" || chunk.Tag == "SCCl" || chunk.Tag == "SCDl" {
			continue
		}
		return fmt.Errorf("chunkUnsupported[%d]: %q", index, chunk.Tag)
	}
	mkvName := strings.TrimSuffix(filepath.Base(destinationPath), filepath.Ext(destinationPath)) + ".mkv"
	definition := fmt.Sprintf("%sMOVIE %s\n\tVERSION %d\n\tMKV %s\n", resourceDSEHeader, strconv.Quote(identity), version, strconv.Quote(mkvName))
	err = os.WriteFile(destinationPath, []byte(definition), 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func projectMovieResources(ctx context.Context, pkg *dbpf.Reader, destinationPath string, manifest *dbpf.Manifest) error {
	for ordinal, resource := range manifest.Resources {
		if resource.Entry.Type != movie.ResourceType {
			continue
		}
		definitionPath, err := safePath(destinationPath, manifest.Resources[ordinal].PayloadPath)
		if err != nil {
			return fmt.Errorf("movieDefinitionPath[%d]: %w", ordinal, err)
		}
		err = projectPackageMovieResource(ctx, pkg, ordinal, definitionPath)
		if err != nil {
			return fmt.Errorf("movieProject[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func projectPackageMovieResource(ctx context.Context, pkg *dbpf.Reader, ordinal int, definitionPath string) error {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return fmt.Errorf("ordinalRange: got %d, resources %d", ordinal, len(pkg.Entries))
	}
	entry := pkg.Entries[ordinal]
	if entry.Type != movie.ResourceType {
		return nil
	}
	payloadReader, err := pkg.Open(entry)
	if err != nil {
		return fmt.Errorf("movieOpen: %w", err)
	}
	payload, err := io.ReadAll(payloadReader)
	if err != nil {
		return fmt.Errorf("movieRead: %w", err)
	}
	document, err := movie.Decode(payload)
	if err != nil {
		return fmt.Errorf("movieDecode: %w", err)
	}
	videoDecoderPath, err := movieVideoDecoderPath()
	if err != nil {
		err = preserveRawMovieResource(definitionPath, payload)
		if err != nil {
			return fmt.Errorf("movieRaw: %w", err)
		}
		return nil
	}
	temporaryPath, err := os.MkdirTemp(filepath.Dir(definitionPath), ".movie-decode-*")
	if err != nil {
		return fmt.Errorf("movieTemporary: %w", err)
	}
	defer os.RemoveAll(temporaryPath)
	temporaryMoviePath := filepath.Join(temporaryPath, "source.vp6")
	err = os.WriteFile(temporaryMoviePath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("movieTemporaryWrite: %w", err)
	}
	mkvPath := strings.TrimSuffix(definitionPath, filepath.Ext(definitionPath)) + ".mkv"
	arguments := []string{"-y", "-v", "error", "-i", temporaryMoviePath}
	if movieHasAudio(document) {
		audioDecoderPath, decoderErr := audioDecoderPath()
		if decoderErr != nil {
			return fmt.Errorf("movieAudioDecoder: %w", decoderErr)
		}
		temporaryWAVPath := filepath.Join(temporaryPath, "source.wav")
		command := exec.CommandContext(ctx, audioDecoderPath, "-i", "-o", temporaryWAVPath, temporaryMoviePath)
		output, decodeErr := command.CombinedOutput()
		if decodeErr != nil {
			return fmt.Errorf("movieAudioDecode: %w: %s", decodeErr, strings.TrimSpace(string(output)))
		}
		arguments = append(arguments, "-i", temporaryWAVPath, "-map", "0:v:0", "-map", "1:a:0", "-c:a", "flac")
	} else {
		arguments = append(arguments, "-map", "0:v:0", "-an")
	}
	arguments = append(arguments, "-c:v", "ffv1", "-level", "3", "-coder", "1", "-context", "1", "-slicecrc", "1", "-frames:v", strconv.FormatUint(uint64(document.Header.FrameCount), 10), mkvPath)
	command := exec.CommandContext(ctx, videoDecoderPath, arguments...)
	output, encodeErr := command.CombinedOutput()
	if encodeErr != nil {
		_ = os.Remove(mkvPath)
		return fmt.Errorf("movieMKVEncode: %w: %s", encodeErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func preserveRawMovieResource(definitionPath string, payload []byte) error {
	definitionPayload, err := os.ReadFile(definitionPath)
	if err != nil {
		return fmt.Errorf("definitionRead: %w", err)
	}
	parser := newParser(bytes.NewReader(definitionPayload))
	definition, err := parser.next()
	if err != nil {
		return fmt.Errorf("definitionParse: %w", err)
	}
	if len(definition) != 2 || definition[0] != "MOVIE" {
		return fmt.Errorf("definition: got %v", definition)
	}
	rawName := strings.TrimSuffix(filepath.Base(definitionPath), filepath.Ext(definitionPath)) + ".vp6"
	rawPath := filepath.Join(filepath.Dir(definitionPath), rawName)
	err = os.WriteFile(rawPath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("rawWrite: %w", err)
	}
	contents := fmt.Sprintf("%sMOVIE %s\n\tVERSION %d\n\tRAW %s\n", resourceDSEHeader, strconv.Quote(definition[1]), version, strconv.Quote(rawName))
	err = os.WriteFile(definitionPath, []byte(contents), 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func movieVideoDecoderPath() (string, error) {
	decoderName := "ffmpeg"
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
		candidatePath := filepath.Join(workingPath, ".cache", "ffmpeg", "bin", decoderName)
		if _, statErr := os.Stat(candidatePath); statErr == nil {
			return candidatePath, nil
		}
	}
	return "", errors.New("videoDecoderMissing: install ffmpeg in PATH or beside darkrun")
}

func movieHasAudio(document *movie.Document) bool {
	for _, chunk := range document.Chunks {
		if chunk.Tag == "SCHl" || chunk.Tag == "SCDl" {
			return true
		}
	}
	return false
}

func readMovieResource(sourcePath, identity string) ([]byte, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("definitionOpen: %w", err)
	}
	defer r.Close()
	parser := newParser(bufio.NewReader(r))
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || definition[0] != "MOVIE" || definition[1] != identity {
		return nil, fmt.Errorf("definition: got %v, want MOVIE %q", definition, identity)
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, fmt.Errorf("versionRead: %w", err)
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	rawFields, isRaw, err := parser.optionalProperty("RAW", 1)
	if err != nil {
		return nil, fmt.Errorf("rawRead: %w", err)
	}
	if isRaw {
		rawPath, pathErr := safePath(filepath.Dir(sourcePath), rawFields[0])
		if pathErr != nil {
			return nil, fmt.Errorf("rawPath: %w", pathErr)
		}
		payload, readErr := os.ReadFile(rawPath)
		if readErr != nil {
			return nil, fmt.Errorf("rawOpen: %w", readErr)
		}
		return payload, nil
	}
	mkvFields, err := parser.property("MKV", 1)
	if err != nil {
		return nil, fmt.Errorf("mkvRead: %w", err)
	}
	trailing, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("definitionTrailing: %w", err)
	}
	if len(trailing) != 0 {
		return nil, fmt.Errorf("definitionTrailing: %v", trailing)
	}
	mkvPath, err := safePath(filepath.Dir(sourcePath), mkvFields[0])
	if err != nil {
		return nil, fmt.Errorf("mkvPath: %w", err)
	}
	fi, err := os.Stat(mkvPath)
	if err != nil {
		return nil, fmt.Errorf("mkvStat: %w", err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("mkvPath: %q is a directory", mkvFields[0])
	}
	return nil, errors.New("mkvCodec: native Go VP6 encoding is not implemented")
}

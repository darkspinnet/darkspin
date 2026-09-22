package ds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
)

type audioManifestResource struct {
	ordinal  int
	resource dbpf.Resource
}

type audioProjectionJob struct {
	instanceID uint64
	snr        audioManifestResource
	sns        audioManifestResource
	isSNSFound bool
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
	jobs := make([]audioProjectionJob, 0, len(snrResources))
	for instanceID, snrRecord := range snrResources {
		header, err := audioResourceHeader(pkg, snrRecord.ordinal)
		if err != nil {
			return fmt.Errorf("audioHeader[%016X]: %w", instanceID, err)
		}
		isRaw := header.Codec == "NONE" || header.Codec == "RESERVED"
		if isRaw {
			continue
		}
		snsRecord, isSNSFound := snsResources[instanceID]
		if header.Storage != "RAM" && !isSNSFound {
			definitionPath, pathErr := safePath(destinationPath, snrRecord.resource.PayloadPath)
			if pathErr != nil {
				return fmt.Errorf("rawPath[%016X]: %w", instanceID, pathErr)
			}
			identity := resourceDefinitionIdentity(snrRecord.resource.PayloadPath)
			pathErr = preserveRawAudioResource(pkg, snrRecord.ordinal, definitionPath, identity+".snr")
			if pathErr != nil {
				return fmt.Errorf("rawWrite[%016X]: %w", instanceID, pathErr)
			}
			continue
		}
		jobs = append(jobs, audioProjectionJob{
			instanceID: instanceID,
			snr:        snrRecord,
			sns:        snsRecord,
			isSNSFound: isSNSFound,
		})
	}
	if len(jobs) == 0 {
		return nil
	}
	jobChannel := make(chan audioProjectionJob, len(jobs))
	resultChannel := make(chan error, len(jobs))
	for _, job := range jobs {
		jobChannel <- job
	}
	close(jobChannel)
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount > 8 {
		workerCount = 8
	}
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		go projectAudioWorker(ctx, pkg, destinationPath, jobChannel, resultChannel, &workers)
	}
	workers.Wait()
	close(resultChannel)
	for projectionErr := range resultChannel {
		if projectionErr != nil {
			return projectionErr
		}
	}
	return nil
}

func projectAudioWorker(ctx context.Context, pkg *dbpf.Reader, destinationPath string, jobs <-chan audioProjectionJob, results chan<- error, workers *sync.WaitGroup) {
	defer workers.Done()
	for job := range jobs {
		err := projectAudioJob(ctx, pkg, destinationPath, job)
		results <- err
	}
}

func projectAudioJob(ctx context.Context, pkg *dbpf.Reader, destinationPath string, job audioProjectionJob) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("audioContext: %w", err)
	}
	snrDefinitionPath, err := safePath(destinationPath, job.snr.resource.PayloadPath)
	if err != nil {
		return fmt.Errorf("snrPath: %w", err)
	}
	identity := resourceDefinitionIdentity(job.snr.resource.PayloadPath)
	snrPayload, err := readDecodedAudioPayload(pkg, job.snr.ordinal)
	if err != nil {
		return fmt.Errorf("snrRead: %w", err)
	}
	var snsPayload []byte
	if job.isSNSFound {
		snsPayload, err = readDecodedAudioPayload(pkg, job.sns.ordinal)
		if err != nil {
			return fmt.Errorf("snsRead: %w", err)
		}
	}
	wav, err := audio.DecodeSNR(snrPayload, snsPayload)
	if err != nil {
		return fmt.Errorf("audioDecode[%016X]: %w", job.instanceID, err)
	}
	wavPayload, err := audio.EncodeWAV(wav)
	if err != nil {
		return fmt.Errorf("waveEncode[%016X]: %w", job.instanceID, err)
	}
	wavPath := filepath.Join(filepath.Dir(snrDefinitionPath), identity+".wav")
	err = os.WriteFile(wavPath, wavPayload, 0o644)
	if err != nil {
		return fmt.Errorf("waveWrite[%016X]: %w", job.instanceID, err)
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
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
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
	header, err := audioResourceHeader(pkg, snrOrdinal)
	if err != nil {
		return fmt.Errorf("audioHeader: %w", err)
	}
	isRaw := header.Codec == "NONE" || header.Codec == "RESERVED"
	if isRaw {
		return nil
	}
	if header.Storage != "RAM" && snsOrdinal < 0 {
		rawName := strings.TrimSuffix(filepath.Base(definitionPath), filepath.Ext(definitionPath)) + ".snr"
		err = preserveRawAudioResource(pkg, snrOrdinal, definitionPath, rawName)
		if err != nil {
			return fmt.Errorf("rawWrite: %w", err)
		}
		return nil
	}
	snrPayload, err := readDecodedAudioPayload(pkg, snrOrdinal)
	if err != nil {
		return fmt.Errorf("snrRead: %w", err)
	}
	var snsPayload []byte
	if snsOrdinal >= 0 {
		snsPayload, err = readDecodedAudioPayload(pkg, snsOrdinal)
		if err != nil {
			return fmt.Errorf("snsRead: %w", err)
		}
	}
	wav, err := audio.DecodeSNR(snrPayload, snsPayload)
	if err != nil {
		return fmt.Errorf("audioDecode: %w", err)
	}
	wavPayload, err := audio.EncodeWAV(wav)
	if err != nil {
		return fmt.Errorf("waveEncode: %w", err)
	}
	wavPath := strings.TrimSuffix(definitionPath, filepath.Ext(definitionPath)) + ".wav"
	err = os.WriteFile(wavPath, wavPayload, 0o644)
	if err != nil {
		return fmt.Errorf("waveWrite: %w", err)
	}
	err = replaceAudioWAV(definitionPath, filepath.Base(wavPath))
	if err != nil {
		return fmt.Errorf("waveReference: %w", err)
	}
	return nil
}

func audioResourceHeader(pkg *dbpf.Reader, ordinal int) (audio.Header, error) {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return audio.Header{}, fmt.Errorf("ordinalRange: %d", ordinal)
	}
	r, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return audio.Header{}, fmt.Errorf("resourceOpen: %w", err)
	}
	headerPayload := make([]byte, 8)
	_, err = io.ReadFull(r, headerPayload)
	if err != nil {
		return audio.Header{}, fmt.Errorf("headerRead: %w", err)
	}
	header, err := audio.DecodeHeader(headerPayload)
	if err != nil {
		return audio.Header{}, fmt.Errorf("headerDecode: %w", err)
	}
	return header, nil
}

func preserveRawAudioResource(pkg *dbpf.Reader, ordinal int, definitionPath, rawName string) error {
	rawPath := filepath.Join(filepath.Dir(definitionPath), rawName)
	err := writeDecodedAudioPath(pkg, ordinal, rawPath)
	if err != nil {
		return fmt.Errorf("payloadWrite: %w", err)
	}
	err = replaceAudioProperty(definitionPath, ordinal, "WAV", "RAW", rawName)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	return nil
}

func replaceAudioProperty(definitionPath string, ordinal int, oldProperty, newProperty, relativePath string) error {
	payload, err := os.ReadFile(definitionPath)
	if err != nil {
		return fmt.Errorf("definitionRead: %w", err)
	}
	body, blocks, err := parseRenderDefinitions(payload)
	if err != nil {
		return fmt.Errorf("definitionParse: %w", err)
	}
	matchCount := 0
	var rewritten strings.Builder
	position := 0
	for _, block := range blocks {
		rewritten.WriteString(body[position:block.start])
		definition := body[block.start:block.end]
		parser := newParser(strings.NewReader(definition))
		_, readErr := parser.next()
		if readErr != nil {
			return fmt.Errorf("definitionHeader: %w", readErr)
		}
		_, readErr = parser.property("VERSION", 1)
		if readErr != nil {
			return fmt.Errorf("definitionVersion: %w", readErr)
		}
		ordinalFields, isOrdinalPresent, readErr := parser.optionalProperty("ORDINAL", 1)
		if readErr != nil {
			return fmt.Errorf("definitionOrdinal: %w", readErr)
		}
		isMatch := false
		if isOrdinalPresent {
			parsedOrdinal, parseErr := strconv.Atoi(ordinalFields[0])
			if parseErr != nil {
				return fmt.Errorf("definitionOrdinal: %w", parseErr)
			}
			isMatch = parsedOrdinal == ordinal
		}
		if isMatch {
			oldPrefix := "\t" + oldProperty + " "
			lineStart := strings.Index(definition, oldPrefix)
			if lineStart < 0 {
				return fmt.Errorf("propertyMissing: %s", oldProperty)
			}
			lineEnd := strings.IndexByte(definition[lineStart:], '\n')
			if lineEnd < 0 {
				lineEnd = len(definition)
			} else {
				lineEnd += lineStart
			}
			definition = definition[:lineStart] + fmt.Sprintf("\t%s %q", newProperty, relativePath) + definition[lineEnd:]
			matchCount++
		}
		rewritten.WriteString(definition)
		position = block.end
	}
	rewritten.WriteString(body[position:])
	if matchCount != 1 {
		return fmt.Errorf("definitionMatches: got %d, want 1", matchCount)
	}
	err = os.WriteFile(definitionPath, []byte(resourceDSEHeader+rewritten.String()), 0o644)
	if err != nil {
		return fmt.Errorf("definitionOutput: %w", err)
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
	return writeDecodedAudioPathWithFlags(pkg, ordinal, destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

func writeDecodedAudioPathWithFlags(pkg *dbpf.Reader, ordinal int, destinationPath string, flags int) error {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return fmt.Errorf("ordinalRange: %d", ordinal)
	}
	payloadReader, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return fmt.Errorf("resourceOpen: %w", err)
	}
	w, err := os.OpenFile(destinationPath, flags, 0o644)
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

func readDecodedAudioPayload(pkg *dbpf.Reader, ordinal int) ([]byte, error) {
	if ordinal < 0 || ordinal >= len(pkg.Entries) {
		return nil, fmt.Errorf("ordinalRange: %d", ordinal)
	}
	r, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return nil, fmt.Errorf("resourceOpen: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("resourceRead: %w", err)
	}
	return payload, nil
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

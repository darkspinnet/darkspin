package audio

import (
	_ "embed"
	"encoding/binary"
	"fmt"
)

//go:embed registry/spore.bin
var sporeRegistryData []byte

//go:embed registry/darkspore.bin
var darksporeRegistryData []byte

type registryReader struct {
	data    []byte
	offset  int
	strings []string
}

type registryResource struct {
	packageName      string
	ordinal          int
	resourceType     uint32
	resourceGroup    uint32
	resourceInstance uint64
}

type registryUsageSpan struct {
	eventInstance uint32
	start         uint32
	count         uint16
}

func newRegistryReader(data []byte, magic string) (*registryReader, error) {
	e := &registryReader{data: data}
	header, err := e.bytes(4)
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	if string(header) != magic {
		return nil, fmt.Errorf("headerMagic: got %q", header)
	}
	stringCount, err := e.u16()
	if err != nil {
		return nil, fmt.Errorf("stringCount: %w", err)
	}
	e.strings = make([]string, stringCount)
	for stringIndex := range e.strings {
		stringLength, readErr := e.u16()
		if readErr != nil {
			return nil, fmt.Errorf("stringLength[%d]: %w", stringIndex, readErr)
		}
		text, readErr := e.bytes(int(stringLength))
		if readErr != nil {
			return nil, fmt.Errorf("stringRead[%d]: %w", stringIndex, readErr)
		}
		e.strings[stringIndex] = string(text)
	}
	return e, nil
}

func (e *registryReader) bytes(size int) ([]byte, error) {
	if size < 0 || e.offset > len(e.data)-size {
		return nil, fmt.Errorf("registryBounds: offset %d size %d length %d", e.offset, size, len(e.data))
	}
	data := e.data[e.offset : e.offset+size]
	e.offset += size
	return data, nil
}

func (e *registryReader) u8() (uint8, error) {
	data, err := e.bytes(1)
	if err != nil {
		return 0, fmt.Errorf("u8Read: %w", err)
	}
	return data[0], nil
}

func (e *registryReader) u16() (uint16, error) {
	data, err := e.bytes(2)
	if err != nil {
		return 0, fmt.Errorf("u16Read: %w", err)
	}
	return binary.LittleEndian.Uint16(data), nil
}

func (e *registryReader) u32() (uint32, error) {
	data, err := e.bytes(4)
	if err != nil {
		return 0, fmt.Errorf("u32Read: %w", err)
	}
	return binary.LittleEndian.Uint32(data), nil
}

func (e *registryReader) u64() (uint64, error) {
	data, err := e.bytes(8)
	if err != nil {
		return 0, fmt.Errorf("u64Read: %w", err)
	}
	return binary.LittleEndian.Uint64(data), nil
}

func (e *registryReader) text() (string, error) {
	stringIndex, err := e.u16()
	if err != nil {
		return "", fmt.Errorf("textIndex: %w", err)
	}
	if int(stringIndex) >= len(e.strings) {
		return "", fmt.Errorf("textBounds: index %d count %d", stringIndex, len(e.strings))
	}
	return e.strings[stringIndex], nil
}

func (e *registryReader) usages() (map[uint32][]SampleUsage, error) {
	resourceCount, err := e.u16()
	if err != nil {
		return nil, fmt.Errorf("resourceCount: %w", err)
	}
	resources := make([]registryResource, resourceCount)
	for resourceIndex := range resources {
		packageName, readErr := e.text()
		if readErr != nil {
			return nil, fmt.Errorf("resourcePackage[%d]: %w", resourceIndex, readErr)
		}
		ordinal, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("resourceOrdinal[%d]: %w", resourceIndex, readErr)
		}
		resourceType, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("resourceType[%d]: %w", resourceIndex, readErr)
		}
		resourceGroup, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("resourceGroup[%d]: %w", resourceIndex, readErr)
		}
		resourceInstance, readErr := e.u64()
		if readErr != nil {
			return nil, fmt.Errorf("resourceInstance[%d]: %w", resourceIndex, readErr)
		}
		resources[resourceIndex] = registryResource{packageName: packageName, ordinal: int(ordinal), resourceType: resourceType, resourceGroup: resourceGroup, resourceInstance: resourceInstance}
	}
	spanCount, err := e.u32()
	if err != nil {
		return nil, fmt.Errorf("usageSpanCount: %w", err)
	}
	spans := make([]registryUsageSpan, spanCount)
	for spanIndex := range spans {
		eventInstance, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("usageEvent[%d]: %w", spanIndex, readErr)
		}
		start, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("usageStart[%d]: %w", spanIndex, readErr)
		}
		count, readErr := e.u16()
		if readErr != nil {
			return nil, fmt.Errorf("usageCount[%d]: %w", spanIndex, readErr)
		}
		spans[spanIndex] = registryUsageSpan{eventInstance: eventInstance, start: start, count: count}
	}
	recordCount, err := e.u32()
	if err != nil {
		return nil, fmt.Errorf("usageRecordCount: %w", err)
	}
	records := make([]SampleUsage, recordCount)
	for recordIndex := range records {
		resourceIndex, readErr := e.u16()
		if readErr != nil {
			return nil, fmt.Errorf("usageResource[%d]: %w", recordIndex, readErr)
		}
		if int(resourceIndex) >= len(resources) {
			return nil, fmt.Errorf("usageResource[%d]: index %d count %d", recordIndex, resourceIndex, len(resources))
		}
		context, readErr := e.text()
		if readErr != nil {
			return nil, fmt.Errorf("usageContext[%d]: %w", recordIndex, readErr)
		}
		referenceKind, readErr := e.text()
		if readErr != nil {
			return nil, fmt.Errorf("usageKind[%d]: %w", recordIndex, readErr)
		}
		byteOffset, readErr := e.u32()
		if readErr != nil {
			return nil, fmt.Errorf("usageOffset[%d]: %w", recordIndex, readErr)
		}
		resource := resources[resourceIndex]
		records[recordIndex] = SampleUsage{PackageName: resource.packageName, Context: context, ReferenceKind: referenceKind, ByteOffset: int(byteOffset), Ordinal: resource.ordinal, ResourceType: resource.resourceType, ResourceGroup: resource.resourceGroup, ResourceInstance: resource.resourceInstance}
	}
	usages := make(map[uint32][]SampleUsage, len(spans))
	for spanIndex, span := range spans {
		end := uint64(span.start) + uint64(span.count)
		if end > uint64(len(records)) {
			return nil, fmt.Errorf("usageSpan[%d]: start %d count %d records %d", spanIndex, span.start, span.count, len(records))
		}
		usages[span.eventInstance] = append([]SampleUsage(nil), records[span.start:uint32(end)]...)
	}
	return usages, nil
}

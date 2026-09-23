package audio

import "fmt"

type sporeAliasSpan struct {
	instanceID uint32
	name       string
	source     string
	start      uint32
	count      uint8
}

func decodeSporeRegistry(data []byte) (map[uint32]string, map[uint32]SampleAlias, map[uint32][]SampleUsage, error) {
	r, err := newRegistryReader(data, "SPK1")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("registryOpen: %w", err)
	}
	archiveName, err := r.text()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("archiveName: %w", err)
	}
	propertyID, err := r.u32()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("propertyID: %w", err)
	}
	propertyName, err := r.text()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("propertyName: %w", err)
	}
	pointerName, err := r.text()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pointerName: %w", err)
	}
	nameCount, err := r.u32()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("nameCount: %w", err)
	}
	names := make(map[uint32]string, nameCount)
	for nameIndex := uint32(0); nameIndex < nameCount; nameIndex++ {
		instanceID, readErr := r.u32()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("nameID[%d]: %w", nameIndex, readErr)
		}
		name, readErr := r.text()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("nameText[%d]: %w", nameIndex, readErr)
		}
		names[instanceID] = name
	}
	usages, err := r.usages()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("usages: %w", err)
	}
	aliasCount, err := r.u32()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aliasCount: %w", err)
	}
	spans := make([]sporeAliasSpan, aliasCount)
	for aliasIndex := range spans {
		instanceID, readErr := r.u32()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("aliasID[%d]: %w", aliasIndex, readErr)
		}
		name, readErr := r.text()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("aliasName[%d]: %w", aliasIndex, readErr)
		}
		source, readErr := r.text()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("aliasSource[%d]: %w", aliasIndex, readErr)
		}
		start, readErr := r.u32()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("aliasStart[%d]: %w", aliasIndex, readErr)
		}
		count, readErr := r.u8()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("aliasReferences[%d]: %w", aliasIndex, readErr)
		}
		spans[aliasIndex] = sporeAliasSpan{instanceID: instanceID, name: name, source: source, start: start, count: count}
	}
	referenceCount, err := r.u32()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("referenceCount: %w", err)
	}
	references := make([]SampleReference, referenceCount)
	for referenceIndex := range references {
		eventInstance, readErr := r.u32()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("referenceEvent[%d]: %w", referenceIndex, readErr)
		}
		eventName, readErr := r.text()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("referenceName[%d]: %w", referenceIndex, readErr)
		}
		itemIndex, readErr := r.u8()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("referenceIndex[%d]: %w", referenceIndex, readErr)
		}
		itemCount, readErr := r.u8()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("referenceItems[%d]: %w", referenceIndex, readErr)
		}
		flags, readErr := r.u8()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("referenceFlags[%d]: %w", referenceIndex, readErr)
		}
		references[referenceIndex] = SampleReference{ArchiveName: archiveName, EventInstance: eventInstance, EventName: eventName, Usages: append([]SampleUsage(nil), usages[eventInstance]...), PropertyID: propertyID, PropertyName: propertyName, PointerName: pointerName, ItemIndex: int(itemIndex), ItemCount: int(itemCount), IsLooped: flags&1 != 0}
	}
	aliases := make(map[uint32]SampleAlias, len(spans))
	for aliasIndex, span := range spans {
		end := uint64(span.start) + uint64(span.count)
		if end > uint64(len(references)) {
			return nil, nil, nil, fmt.Errorf("aliasSpan[%d]: start %d count %d references %d", aliasIndex, span.start, span.count, len(references))
		}
		aliases[span.instanceID] = SampleAlias{Name: span.name, Source: span.source, References: append([]SampleReference(nil), references[span.start:uint32(end)]...)}
	}
	if r.offset != len(r.data) {
		return nil, nil, nil, fmt.Errorf("registryTrailing: %d bytes", len(r.data)-r.offset)
	}
	return names, aliases, usages, nil
}

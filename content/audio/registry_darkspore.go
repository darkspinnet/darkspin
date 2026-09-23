package audio

import "fmt"

func decodeDarksporeRegistry(data []byte) (map[uint32]string, map[uint32][]SampleUsage, error) {
	r, err := newRegistryReader(data, "DPK1")
	if err != nil {
		return nil, nil, fmt.Errorf("registryOpen: %w", err)
	}
	nameCount, err := r.u32()
	if err != nil {
		return nil, nil, fmt.Errorf("nameCount: %w", err)
	}
	names := make(map[uint32]string, nameCount)
	for nameIndex := uint32(0); nameIndex < nameCount; nameIndex++ {
		eventInstance, readErr := r.u32()
		if readErr != nil {
			return nil, nil, fmt.Errorf("nameID[%d]: %w", nameIndex, readErr)
		}
		name, readErr := r.text()
		if readErr != nil {
			return nil, nil, fmt.Errorf("nameText[%d]: %w", nameIndex, readErr)
		}
		names[eventInstance] = name
	}
	usages, err := r.usages()
	if err != nil {
		return nil, nil, fmt.Errorf("usages: %w", err)
	}
	if r.offset != len(r.data) {
		return nil, nil, fmt.Errorf("registryTrailing: %d bytes", len(r.data)-r.offset)
	}
	return names, usages, nil
}

package animation

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const dseVersion = 1

func WriteDSE(w io.Writer, identity string, document *Document) error {
	if document == nil {
		return errors.New("document: nil")
	}
	_, err := fmt.Fprintf(w, "// darkspin resource dse v1\nANIMATION %q\n\tVERSION %d\n\tFORMATVERSION %d\n\tSOURCE %q\n\tRESOURCEID 0x%08X\n\tHEADERFLAGS 0x%08X\n\tFRAMESTEP %s\n\tLENGTH %s\n\tPREDICATEFLAGS 0x%08X\n\tPREDICATESTATE 0x%08X\n\tNUMEVENTS %d\n", identity, dseVersion, document.FormatVersion, document.Source, document.ResourceID, document.HeaderFlags, floatText(document.FrameStep), floatText(document.Length), document.Predicate.Flags, document.Predicate.State, len(document.Events))
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	for eventIndex, event := range document.Events {
		_, err = fmt.Fprintf(w, "\t\tEVENT %d\n\t\t\tNAME? %s\n\t\t\tFLAGS 0x%08X\n", eventIndex, nullableText(event.Name), event.Flags)
		if err != nil {
			return fmt.Errorf("event[%d]Write: %w", eventIndex, err)
		}
		for selectorIndex, selector := range event.Selectors {
			_, err = fmt.Fprintf(w, "\t\t\tSELECTOR %d\n\t\t\t\tFLAGS 0x%08X\n\t\t\t\tSELECTFLAGS 0x%08X\n\t\t\t\tCAPABILITY? %s\n", selectorIndex, selector.Flags, selector.SelectFlags, nullableText(selector.Capability))
			if err != nil {
				return fmt.Errorf("event[%d]Selector[%d]Write: %w", eventIndex, selectorIndex, err)
			}
		}
		_, err = fmt.Fprintf(w, "\t\t\tARCHETYPE? %s\n\t\t\tEVENTGROUP 0x%08X\n\t\t\tID 0x%08X\n\t\t\tPARAMETER0 0x%08X\n\t\t\tMAXSQRDIST %s\n\t\t\tPARAMETER1 0x%08X\n\t\t\tPREDICATEFLAGS 0x%08X\n\t\t\tPREDICATESTATE 0x%08X\n", nullableText(event.Archetype), event.EventGroup, event.ID, event.Parameter0, floatText(event.MaxSqrDist), event.Parameter1, event.Predicate.Flags, event.Predicate.State)
		if err != nil {
			return fmt.Errorf("event[%d]FieldsWrite: %w", eventIndex, err)
		}
	}
	_, err = fmt.Fprintf(w, "\tNUMCHANNELS %d\n", len(document.Channels))
	if err != nil {
		return fmt.Errorf("channelCountWrite: %w", err)
	}
	for channelIndex, channel := range document.Channels {
		err = writeChannelDSE(w, channelIndex, channel)
		if err != nil {
			return fmt.Errorf("channel[%d]Write: %w", channelIndex, err)
		}
	}
	return nil
}

func writeChannelDSE(w io.Writer, channelIndex int, channel Channel) error {
	_, err := fmt.Fprintf(w, "\t\tCHANNEL %q // %d\n\t\t\tMOVEMENTFLAGS 0x%08X\n", channel.Name, channelIndex, channel.MovementFlags)
	if err != nil {
		return err
	}
	err = writeSelectorDSE(w, "PRIMARYSELECTOR", channel.PrimarySelector)
	if err != nil {
		return err
	}
	err = writeSelectorDSE(w, "SECONDARYSELECTOR", channel.SecondarySelector)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "\t\t\tBINDFLAGS 0x%08X\n\t\t\tKEYFRAMECOUNT %d\n\t\t\tNUMCOMPONENTS %d\n", channel.BindFlags, channel.KeyframeCount, len(channel.Components))
	if err != nil {
		return err
	}
	for componentIndex, component := range channel.Components {
		declaration, declarationErr := componentDeclaration(component.Type())
		if declarationErr != nil {
			return fmt.Errorf("component[%d]: %w", componentIndex, declarationErr)
		}
		_, err = fmt.Fprintf(w, "\t\t\t\t%s %d\n\t\t\t\t\tCOMPONENTFLAGS 0x%08X\n\t\t\t\t\tID 0x%08X\n\t\t\t\t\tINDEX %d\n", declaration, componentIndex, component.Flags&^0xF, component.ID, component.Index)
		if err != nil {
			return fmt.Errorf("component[%d]Write: %w", componentIndex, err)
		}
		for frameIndex, keyframe := range component.Keyframes {
			err = writeKeyframeDSE(w, declaration, frameIndex, keyframe)
			if err != nil {
				return fmt.Errorf("component[%d]Frame[%d]Write: %w", componentIndex, frameIndex, err)
			}
		}
	}
	return nil
}

func writeSelectorDSE(w io.Writer, declaration string, selector Selector) error {
	_, err := fmt.Fprintf(w, "\t\t\t%s\n\t\t\t\tFLAGS 0x%08X\n\t\t\t\tCAPABILITY? %s\n\t\t\t\tFIELD8 0x%08X\n\t\t\t\tFIELDC 0x%08X\n", declaration, selector.Flags, nullableText(selector.Capability), selector.Field8, selector.FieldC)
	return err
}

func writeKeyframeDSE(w io.Writer, declaration string, frameIndex int, keyframe Keyframe) error {
	switch declaration {
	case "INFO":
		_, err := fmt.Fprintf(w, "\t\t\t\t\t\tINFOFRAME %d\n\t\t\t\t\t\t\tTIME %d\n\t\t\t\t\t\t\tEVENTSTART %d\n\t\t\t\t\t\t\tEVENTCOUNT %d\n\t\t\t\t\t\t\tFLAGS 0x%08X\n", frameIndex, keyframe.Time, keyframe.EventStart, keyframe.EventCount, keyframe.Flags)
		return err
	case "POSITION":
		_, err := fmt.Fprintf(w, "\t\t\t\t\t\tPOSITIONFRAME %d\n\t\t\t\t\t\t\tPOSITION %s %s %s\n\t\t\t\t\t\t\tWEIGHT %s\n", frameIndex, floatText(keyframe.Position[0]), floatText(keyframe.Position[1]), floatText(keyframe.Position[2]), floatText(keyframe.Weight))
		if err != nil {
			return err
		}
		return writeInterpolatorsDSE(w, keyframe.Interpolators)
	case "ROTATION":
		_, err := fmt.Fprintf(w, "\t\t\t\t\t\tROTATIONFRAME %d\n\t\t\t\t\t\t\tROTATION %s %s %s %s\n\t\t\t\t\t\t\tWEIGHT %s\n\t\t\t\t\t\t\tNEXTMODE %d\n\t\t\t\t\t\t\tPREVIOUSMODE %d\n", frameIndex, floatText(keyframe.Rotation[0]), floatText(keyframe.Rotation[1]), floatText(keyframe.Rotation[2]), floatText(keyframe.Rotation[3]), floatText(keyframe.Weight), keyframe.NextMode, keyframe.PreviousMode)
		if err != nil {
			return err
		}
		return writeInterpolatorsDSE(w, keyframe.Interpolators)
	case "RIGBLOCK":
		_, err := fmt.Fprintf(w, "\t\t\t\t\t\tRIGBLOCKFRAME %d\n\t\t\t\t\t\t\tSCALAR %s\n\t\t\t\t\t\t\tWEIGHT %s\n", frameIndex, floatText(keyframe.Scalar), floatText(keyframe.Weight))
		if err != nil {
			return err
		}
		return writeInterpolatorsDSE(w, keyframe.Interpolators)
	default:
		return fmt.Errorf("declaration: %s", declaration)
	}
}

func writeInterpolatorsDSE(w io.Writer, interpolators []Interpolator) error {
	var err error
	for index, interpolator := range interpolators {
		_, err = fmt.Fprintf(w, "\t\t\t\t\t\t\t\tINTERPOLATOR %d\n\t\t\t\t\t\t\t\t\tNEXTFACTOR %s\n\t\t\t\t\t\t\t\t\tPREVIOUSFACTOR %s\n\t\t\t\t\t\t\t\t\tNEXTMODE %d\n\t\t\t\t\t\t\t\t\tPREVIOUSMODE %d\n", index, floatText(interpolator.NextFactor), floatText(interpolator.PreviousFactor), interpolator.NextMode, interpolator.PreviousMode)
		if err != nil {
			return err
		}
	}
	return nil
}

func componentDeclaration(componentType uint32) (string, error) {
	switch componentType {
	case 0:
		return "INFO", nil
	case 1:
		return "POSITION", nil
	case 2:
		return "ROTATION", nil
	case 3:
		return "RIGBLOCK", nil
	default:
		return "", fmt.Errorf("type: %d", componentType)
	}
}

func floatText(number float32) string { return strconv.FormatFloat(float64(number), 'g', -1, 32) }

func nullableText(text string) string {
	if text == "" {
		return "NULL"
	}
	return strconv.Quote(text)
}

type dseParser struct {
	scanner *bufio.Scanner
	line    int
}

func ReadDSE(r io.Reader, identity string) ([]byte, error) {
	parser := &dseParser{scanner: bufio.NewScanner(r)}
	declaration, err := parser.fields()
	if err != nil {
		return nil, fmt.Errorf("declarationRead: %w", err)
	}
	if len(declaration) != 2 || declaration[0] != "ANIMATION" || declaration[1] != identity {
		return nil, fmt.Errorf("declaration: got %v, want ANIMATION %q", declaration, identity)
	}
	document := &Document{}
	version, err := parser.uint32("VERSION")
	if err != nil {
		return nil, err
	}
	if version != dseVersion {
		return nil, fmt.Errorf("version: %d", version)
	}
	document.FormatVersion, err = parser.uint32("FORMATVERSION")
	if err != nil {
		return nil, err
	}
	document.Source, err = parser.text("SOURCE")
	if err != nil {
		return nil, err
	}
	document.ResourceID, err = parser.uint32("RESOURCEID")
	if err != nil {
		return nil, err
	}
	document.HeaderFlags, err = parser.uint32("HEADERFLAGS")
	if err != nil {
		return nil, err
	}
	document.FrameStep, err = parser.float32("FRAMESTEP")
	if err != nil {
		return nil, err
	}
	document.Length, err = parser.float32("LENGTH")
	if err != nil {
		return nil, err
	}
	document.Predicate.Flags, err = parser.uint32("PREDICATEFLAGS")
	if err != nil {
		return nil, err
	}
	document.Predicate.State, err = parser.uint32("PREDICATESTATE")
	if err != nil {
		return nil, err
	}
	eventCount, err := parser.count("NUMEVENTS")
	if err != nil {
		return nil, err
	}
	document.Events = make([]Event, eventCount)
	for eventIndex := range document.Events {
		event, eventErr := parser.event(eventIndex)
		if eventErr != nil {
			return nil, fmt.Errorf("event[%d]: %w", eventIndex, eventErr)
		}
		document.Events[eventIndex] = event
	}
	channelCount, err := parser.count("NUMCHANNELS")
	if err != nil {
		return nil, err
	}
	document.Channels = make([]Channel, channelCount)
	for channelIndex := range document.Channels {
		channel, channelErr := parser.channel(channelIndex)
		if channelErr != nil {
			return nil, fmt.Errorf("channel[%d]: %w", channelIndex, channelErr)
		}
		document.Channels[channelIndex] = channel
	}
	extra, extraErr := parser.fields()
	if !errors.Is(extraErr, io.EOF) {
		if extraErr != nil {
			return nil, fmt.Errorf("trailingRead: %w", extraErr)
		}
		return nil, fmt.Errorf("trailing: %v", extra)
	}
	payload, err := Encode(document)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return payload, nil
}

func (e *dseParser) event(index int) (Event, error) {
	fields, err := e.expect("EVENT", 1)
	if err != nil {
		return Event{}, err
	}
	if fields[0] != strconv.Itoa(index) {
		return Event{}, fmt.Errorf("index: got %s, want %d", fields[0], index)
	}
	event := Event{}
	event.Name, err = e.nullable("NAME?")
	if err != nil {
		return Event{}, err
	}
	event.Flags, err = e.uint32("FLAGS")
	if err != nil {
		return Event{}, err
	}
	for selectorIndex := range event.Selectors {
		selectorFields, selectorErr := e.expect("SELECTOR", 1)
		if selectorErr != nil {
			return Event{}, selectorErr
		}
		if selectorFields[0] != strconv.Itoa(selectorIndex) {
			return Event{}, fmt.Errorf("selectorIndex: got %s, want %d", selectorFields[0], selectorIndex)
		}
		event.Selectors[selectorIndex].Flags, err = e.uint32("FLAGS")
		if err != nil {
			return Event{}, err
		}
		event.Selectors[selectorIndex].SelectFlags, err = e.uint32("SELECTFLAGS")
		if err != nil {
			return Event{}, err
		}
		event.Selectors[selectorIndex].Capability, err = e.nullable("CAPABILITY?")
		if err != nil {
			return Event{}, err
		}
	}
	event.Archetype, err = e.nullable("ARCHETYPE?")
	if err != nil {
		return Event{}, err
	}
	event.EventGroup, err = e.uint32("EVENTGROUP")
	if err != nil {
		return Event{}, err
	}
	event.ID, err = e.uint32("ID")
	if err != nil {
		return Event{}, err
	}
	event.Parameter0, err = e.uint32("PARAMETER0")
	if err != nil {
		return Event{}, err
	}
	event.MaxSqrDist, err = e.float32("MAXSQRDIST")
	if err != nil {
		return Event{}, err
	}
	event.Parameter1, err = e.uint32("PARAMETER1")
	if err != nil {
		return Event{}, err
	}
	event.Predicate.Flags, err = e.uint32("PREDICATEFLAGS")
	if err != nil {
		return Event{}, err
	}
	event.Predicate.State, err = e.uint32("PREDICATESTATE")
	return event, err
}

func (e *dseParser) channel(index int) (Channel, error) {
	fields, err := e.expect("CHANNEL", 1)
	if err != nil {
		return Channel{}, err
	}
	channel := Channel{Name: fields[0]}
	channel.MovementFlags, err = e.uint32("MOVEMENTFLAGS")
	if err != nil {
		return Channel{}, err
	}
	channel.PrimarySelector, err = e.selector("PRIMARYSELECTOR")
	if err != nil {
		return Channel{}, err
	}
	channel.SecondarySelector, err = e.selector("SECONDARYSELECTOR")
	if err != nil {
		return Channel{}, err
	}
	channel.BindFlags, err = e.uint32("BINDFLAGS")
	if err != nil {
		return Channel{}, err
	}
	channel.KeyframeCount, err = e.uint32("KEYFRAMECOUNT")
	if err != nil {
		return Channel{}, err
	}
	componentCount, err := e.count("NUMCOMPONENTS")
	if err != nil {
		return Channel{}, err
	}
	channel.Components = make([]Component, componentCount)
	for componentIndex := range channel.Components {
		component, componentErr := e.component(componentIndex, channel.KeyframeCount)
		if componentErr != nil {
			return Channel{}, fmt.Errorf("component[%d]: %w", componentIndex, componentErr)
		}
		channel.Components[componentIndex] = component
	}
	_ = index
	return channel, nil
}

func (e *dseParser) selector(declaration string) (Selector, error) {
	_, err := e.expect(declaration, 0)
	if err != nil {
		return Selector{}, err
	}
	selector := Selector{}
	selector.Flags, err = e.uint32("FLAGS")
	if err != nil {
		return Selector{}, err
	}
	selector.Capability, err = e.nullable("CAPABILITY?")
	if err != nil {
		return Selector{}, err
	}
	selector.Field8, err = e.uint32("FIELD8")
	if err != nil {
		return Selector{}, err
	}
	selector.FieldC, err = e.uint32("FIELDC")
	return selector, err
}

func (e *dseParser) component(index int, frameCount uint32) (Component, error) {
	fields, err := e.fields()
	if err != nil {
		return Component{}, err
	}
	if len(fields) != 2 || fields[1] != strconv.Itoa(index) {
		return Component{}, fmt.Errorf("declaration: %v", fields)
	}
	componentType := uint32(0)
	switch fields[0] {
	case "INFO":
		componentType = 0
	case "POSITION":
		componentType = 1
	case "ROTATION":
		componentType = 2
	case "RIGBLOCK":
		componentType = 3
	default:
		return Component{}, fmt.Errorf("declaration: %s", fields[0])
	}
	component := Component{}
	component.Flags, err = e.uint32("COMPONENTFLAGS")
	if err != nil {
		return Component{}, err
	}
	component.Flags = component.Flags&^0xF | componentType
	component.ID, err = e.uint32("ID")
	if err != nil {
		return Component{}, err
	}
	component.Index, err = e.uint32("INDEX")
	if err != nil {
		return Component{}, err
	}
	// Keep counts representable on both 32-bit and 64-bit hosts.
	if frameCount > math.MaxInt32 {
		return Component{}, fmt.Errorf("frameCount: %d exceeds supported integer range", frameCount)
	}
	count := int(frameCount)
	component.Keyframes = make([]Keyframe, count)
	for frameIndex := range component.Keyframes {
		keyframe, frameErr := e.keyframe(fields[0], frameIndex)
		if frameErr != nil {
			return Component{}, fmt.Errorf("frame[%d]: %w", frameIndex, frameErr)
		}
		component.Keyframes[frameIndex] = keyframe
	}
	return component, nil
}

func (e *dseParser) keyframe(componentDeclaration string, index int) (Keyframe, error) {
	frameDeclaration := componentDeclaration + "FRAME"
	fields, err := e.expect(frameDeclaration, 1)
	if err != nil {
		return Keyframe{}, err
	}
	if fields[0] != strconv.Itoa(index) {
		return Keyframe{}, fmt.Errorf("index: got %s, want %d", fields[0], index)
	}
	keyframe := Keyframe{}
	switch componentDeclaration {
	case "INFO":
		time, parseErr := e.int32("TIME")
		if parseErr != nil {
			return Keyframe{}, parseErr
		}
		keyframe.Time = time
		keyframe.EventStart, err = e.uint16("EVENTSTART")
		if err != nil {
			return Keyframe{}, fmt.Errorf("eventStartRead: %w", err)
		}
		keyframe.EventCount, err = e.uint8("EVENTCOUNT")
		if err != nil {
			return Keyframe{}, fmt.Errorf("eventCountRead: %w", err)
		}
		keyframe.Flags, err = e.uint32("FLAGS")
	case "POSITION":
		keyframe.Position, err = e.vector3("POSITION")
		if err == nil {
			keyframe.Weight, err = e.float32("WEIGHT")
		}
		if err == nil {
			keyframe.Interpolators, err = e.interpolators(4)
		}
	case "ROTATION":
		keyframe.Rotation, err = e.vector4("ROTATION")
		if err == nil {
			keyframe.Weight, err = e.float32("WEIGHT")
		}
		if err == nil {
			keyframe.NextMode, err = e.uint8("NEXTMODE")
		}
		if err == nil {
			keyframe.PreviousMode, err = e.uint8("PREVIOUSMODE")
		}
		if err == nil {
			keyframe.Interpolators, err = e.interpolators(1)
		}
	case "RIGBLOCK":
		keyframe.Scalar, err = e.float32("SCALAR")
		if err == nil {
			keyframe.Weight, err = e.float32("WEIGHT")
		}
		if err == nil {
			keyframe.Interpolators, err = e.interpolators(2)
		}
	}
	if err != nil {
		return Keyframe{}, fmt.Errorf("frameRead: %w", err)
	}
	return keyframe, nil
}

func (e *dseParser) interpolators(expected int) ([]Interpolator, error) {
	count := expected
	var err error
	interpolators := make([]Interpolator, count)
	for index := range interpolators {
		fields, readErr := e.expect("INTERPOLATOR", 1)
		if readErr != nil {
			return nil, readErr
		}
		if fields[0] != strconv.Itoa(index) {
			return nil, fmt.Errorf("interpolatorIndex: got %s, want %d", fields[0], index)
		}
		interpolators[index].NextFactor, err = e.float32("NEXTFACTOR")
		if err != nil {
			return nil, err
		}
		interpolators[index].PreviousFactor, err = e.float32("PREVIOUSFACTOR")
		if err != nil {
			return nil, err
		}
		interpolators[index].NextMode, err = e.uint8("NEXTMODE")
		if err != nil {
			return nil, fmt.Errorf("nextModeRead[%d]: %w", index, err)
		}
		interpolators[index].PreviousMode, err = e.uint8("PREVIOUSMODE")
		if err != nil {
			return nil, fmt.Errorf("previousModeRead[%d]: %w", index, err)
		}
	}
	return interpolators, nil
}

func (e *dseParser) fields() ([]string, error) {
	for e.scanner.Scan() {
		e.line++
		line := strings.TrimSpace(e.scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields, err := splitFields(line)
		if err != nil {
			return nil, fmt.Errorf("line[%d]: %w", e.line, err)
		}
		return fields, nil
	}
	if err := e.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return nil, io.EOF
}

func splitFields(line string) ([]string, error) {
	fields := make([]string, 0, 4)
	for len(line) > 0 {
		line = strings.TrimLeft(line, " \t")
		if line == "" || strings.HasPrefix(line, "//") {
			break
		}
		if line[0] != '"' {
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				fields = append(fields, line)
				break
			}
			fields = append(fields, line[:end])
			line = line[end:]
			continue
		}
		end := 1
		for end < len(line) {
			if line[end] == '\\' {
				end += 2
				continue
			}
			if line[end] == '"' {
				break
			}
			end++
		}
		if end >= len(line) {
			return nil, errors.New("quote: unterminated")
		}
		decoded, err := strconv.Unquote(line[:end+1])
		if err != nil {
			return nil, fmt.Errorf("quote: %w", err)
		}
		fields = append(fields, decoded)
		line = line[end+1:]
	}
	return fields, nil
}

func (e *dseParser) expect(name string, count int) ([]string, error) {
	fields, err := e.fields()
	if err != nil {
		return nil, fmt.Errorf("%sRead: %w", name, err)
	}
	if len(fields) != count+1 || fields[0] != name {
		return nil, fmt.Errorf("line[%d]: expected %s with %d arguments, got %v", e.line, name, count, fields)
	}
	return fields[1:], nil
}

func (e *dseParser) uint8(name string) (uint8, error) {
	number, err := e.uint32(name)
	if err != nil {
		return 0, fmt.Errorf("uint8Read: %w", err)
	}
	if number > math.MaxUint8 {
		return 0, fmt.Errorf("uint8Range: %s %d exceeds 255", name, number)
	}
	return uint8(number), nil
}

func (e *dseParser) uint16(name string) (uint16, error) {
	number, err := e.uint32(name)
	if err != nil {
		return 0, fmt.Errorf("uint16Read: %w", err)
	}
	if number > math.MaxUint16 {
		return 0, fmt.Errorf("uint16Range: %s %d exceeds 65535", name, number)
	}
	return uint16(number), nil
}

func (e *dseParser) uint32(name string) (uint32, error) {
	fields, err := e.expect(name, 1)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseUint(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return uint32(number), nil
}
func (e *dseParser) int32(name string) (int32, error) {
	fields, err := e.expect(name, 1)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseInt(fields[0], 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return int32(number), nil
}
func (e *dseParser) count(name string) (int, error) {
	number, err := e.uint32(name)
	if err != nil {
		return 0, fmt.Errorf("countRead: %w", err)
	}
	if number > math.MaxInt32 {
		return 0, fmt.Errorf("countRange: %s %d exceeds supported integer range", name, number)
	}
	return int(number), nil
}
func (e *dseParser) float32(name string) (float32, error) {
	fields, err := e.expect(name, 1)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseFloat(fields[0], 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return float32(number), nil
}
func (e *dseParser) text(name string) (string, error) {
	fields, err := e.expect(name, 1)
	if err != nil {
		return "", err
	}
	return fields[0], nil
}
func (e *dseParser) nullable(name string) (string, error) {
	text, err := e.text(name)
	if err != nil {
		return "", err
	}
	if text == "NULL" {
		return "", nil
	}
	return text, nil
}
func (e *dseParser) vector3(name string) ([3]float32, error) {
	fields, err := e.expect(name, 3)
	if err != nil {
		return [3]float32{}, err
	}
	var vector [3]float32
	for index := range vector {
		number, parseErr := strconv.ParseFloat(fields[index], 32)
		if parseErr != nil {
			return vector, fmt.Errorf("%s[%d]Parse: %w", name, index, parseErr)
		}
		vector[index] = float32(number)
	}
	return vector, nil
}
func (e *dseParser) vector4(name string) ([4]float32, error) {
	fields, err := e.expect(name, 4)
	if err != nil {
		return [4]float32{}, err
	}
	var vector [4]float32
	for index := range vector {
		number, parseErr := strconv.ParseFloat(fields[index], 32)
		if parseErr != nil {
			return vector, fmt.Errorf("%s[%d]Parse: %w", name, index, parseErr)
		}
		vector[index] = float32(number)
	}
	return vector, nil
}

package game

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var numericXMLReference = regexp.MustCompile(`&#(?:[xX][0-9a-fA-F]+|[0-9]+);`)

func unmarshalAssetXML(contents []byte, target any) error {
	text := string(contents)
	if !utf8.Valid(contents) {
		var converted strings.Builder
		converted.Grow(len(contents))
		for _, value := range contents {
			converted.WriteRune(rune(value))
		}
		text = converted.String()
	}

	text = numericXMLReference.ReplaceAllStringFunc(text, sanitizeNumericXMLReference)
	var sanitized strings.Builder
	sanitized.Grow(len(text))
	for _, value := range text {
		if validXMLRune(value) {
			sanitized.WriteRune(value)
			continue
		}
		sanitized.WriteRune(utf8.RuneError)
	}

	err := xml.Unmarshal([]byte(sanitized.String()), target)
	if err != nil {
		return fmt.Errorf("assetUnmarshal: %w", err)
	}
	return nil
}

func sanitizeNumericXMLReference(reference string) string {
	digits := reference[2 : len(reference)-1]
	base := 10
	if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
		base = 16
		digits = digits[1:]
	}
	value, err := strconv.ParseInt(digits, base, 32)
	if err != nil || !validXMLRune(rune(value)) {
		return string(utf8.RuneError)
	}
	return reference
}

func validXMLRune(value rune) bool {
	return value == '\t' || value == '\n' || value == '\r' ||
		value >= 0x20 && value <= 0xd7ff ||
		value >= 0xe000 && value <= 0xfffd ||
		value >= 0x10000 && value <= 0x10ffff
}
